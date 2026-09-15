/*
The HR attachment store.

Files hang off whatever record they are evidence for -- an employee's contract,
a leave request's medical note, a payroll run's bank schedule. See
migrations/024_attachments.sql for why the bytes live in Postgres and why the
owner is three text columns rather than a foreign key.

The caps here are the boundary, not the browser's. The app checks a file's size
and type before uploading, which is a convenience for the person choosing it;
this is what actually decides, because a client-side check is advice to a
caller who may not take it.
*/
package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	// MaxAttachmentBytes is one file. Matches ATTACHMENT_MAX_BYTES in the app,
	// so a file the uploader accepted is not refused after it has been sent.
	MaxAttachmentBytes = 1_500_000
	// MaxRecordAttachmentBytes is everything hanging off one record.
	MaxRecordAttachmentBytes = 8_000_000
)

// Attachment is one stored file, without its bytes. Listing a record's files
// must not read megabytes to answer what is essentially a directory.
type Attachment struct {
	ID             uuid.UUID `json:"id"`
	OwnerModule    string    `json:"owner_module"`
	OwnerEntity    string    `json:"owner_entity"`
	OwnerRecordID  string    `json:"owner_record_id"`
	Filename       string    `json:"filename"`
	MIME           string    `json:"mime"`
	SizeBytes      int64     `json:"size_bytes"`
	ChecksumSHA256 string    `json:"checksum_sha256"`
	UploadedBy     string    `json:"uploaded_by_employee_no"`
	CreatedAt      time.Time `json:"created_at"`
}

// AttachmentContent is one stored file with its bytes, for a download.
type AttachmentContent struct {
	Attachment
	Content []byte `json:"-"`
}

/*
allowedAttachmentMIME mirrors ATTACHMENT_ACCEPT in the app.

An allow-list rather than a deny-list: the question is which types HR has a
reason to keep, and anything not on it is a file somebody should be asked about
rather than one this service should guess at. Notably absent is anything the
browser will execute if it is ever served back inline -- SVG included, which is
a document to a person and a script to a renderer.
*/
var allowedAttachmentMIME = map[string]bool{
	"application/pdf":  true,
	"image/jpeg":       true,
	"image/png":        true,
	"image/webp":       true,
	"image/heic":       true,
	"text/plain":       true,
	"text/csv":         true,
	"application/msword": true,
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": true,
	"application/vnd.ms-excel":                                               true,
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":      true,
}

type CreateAttachmentInput struct {
	OwnerModule   string
	OwnerEntity   string
	OwnerRecordID string
	Filename      string
	MIME          string
	Content       []byte
	UploadedBy    string
}

/*
sanitizeFilename keeps a name that is safe to put in a Content-Disposition
header and safe to write to a disk that is not this one.

Path separators and control characters are removed rather than escaped: a
filename is a label here, never a location, and the only thing an operator loses
is a character that was never going to survive a download anyway.
*/
func sanitizeFilename(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "\\", "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	var b strings.Builder
	for _, r := range name {
		if r < 32 || r == 127 || r == '"' {
			continue
		}
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	if out == "" || out == "." || out == ".." {
		return "attachment"
	}
	if len(out) > 200 {
		out = out[:200]
	}
	return out
}

func (s *Store) CreateAttachment(ctx context.Context, in CreateAttachmentInput) (*Attachment, error) {
	if len(in.Content) == 0 {
		return nil, fmt.Errorf("%w: empty file", ErrBadInput)
	}
	if len(in.Content) > MaxAttachmentBytes {
		return nil, fmt.Errorf("%w: file is larger than %d bytes",
			ErrBadInput, MaxAttachmentBytes)
	}
	mime := strings.ToLower(strings.TrimSpace(in.MIME))
	if i := strings.Index(mime, ";"); i >= 0 {
		mime = strings.TrimSpace(mime[:i])
	}
	if !allowedAttachmentMIME[mime] {
		return nil, fmt.Errorf("%w: %q is not an accepted attachment type", ErrBadInput, mime)
	}

	// The per-record ceiling is checked against what is already stored, so a
	// caller cannot walk past it one acceptable file at a time.
	if in.OwnerRecordID != "" {
		var used int64
		if err := s.pool.QueryRow(ctx, `
			SELECT COALESCE(SUM(size_bytes), 0) FROM erp_attachments
			WHERE owner_module = $1 AND owner_entity = $2 AND owner_record_id = $3`,
			in.OwnerModule, in.OwnerEntity, in.OwnerRecordID).Scan(&used); err != nil {
			return nil, err
		}
		if used+int64(len(in.Content)) > MaxRecordAttachmentBytes {
			return nil, fmt.Errorf(
				"%w: this record already holds %d bytes of attachments, and the limit is %d",
				ErrConflict, used, MaxRecordAttachmentBytes)
		}
	}

	sum := sha256.Sum256(in.Content)
	a := Attachment{
		OwnerModule:    strings.TrimSpace(in.OwnerModule),
		OwnerEntity:    strings.TrimSpace(in.OwnerEntity),
		OwnerRecordID:  strings.TrimSpace(in.OwnerRecordID),
		Filename:       sanitizeFilename(in.Filename),
		MIME:           mime,
		SizeBytes:      int64(len(in.Content)),
		ChecksumSHA256: hex.EncodeToString(sum[:]),
		UploadedBy:     strings.TrimSpace(in.UploadedBy),
	}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO erp_attachments
			(owner_module, owner_entity, owner_record_id, filename, mime,
			 size_bytes, checksum_sha256, content, uploaded_by_employee_no)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING id, created_at`,
		a.OwnerModule, a.OwnerEntity, a.OwnerRecordID, a.Filename, a.MIME,
		a.SizeBytes, a.ChecksumSHA256, in.Content, a.UploadedBy).
		Scan(&a.ID, &a.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

const attachmentColumns = `id, owner_module, owner_entity, owner_record_id,
	filename, mime, size_bytes, checksum_sha256, uploaded_by_employee_no, created_at`

func scanAttachment(row pgx.Row) (*Attachment, error) {
	var a Attachment
	if err := row.Scan(&a.ID, &a.OwnerModule, &a.OwnerEntity, &a.OwnerRecordID,
		&a.Filename, &a.MIME, &a.SizeBytes, &a.ChecksumSHA256,
		&a.UploadedBy, &a.CreatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &a, nil
}

// ListAttachments answers what a record holds, without reading the bytes.
func (s *Store) ListAttachments(ctx context.Context, module, entity, recordID string) ([]Attachment, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+attachmentColumns+` FROM erp_attachments
		WHERE ($1 = '' OR owner_module = $1)
		  AND ($2 = '' OR owner_entity = $2)
		  AND ($3 = '' OR owner_record_id = $3)
		ORDER BY created_at DESC
		LIMIT 500`, module, entity, recordID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Attachment{}
	for rows.Next() {
		// Through the same reader the single-row get uses: one column list, one
		// place that knows its shape.
		a, err := scanAttachment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

/*
GetAttachment reads one file, bytes included. The only path that does.

Metadata and content are two queries, both by primary key, so that the wide
column list keeps exactly one reader -- scanAttachment. A second hand-written
copy of it here is how a column added to one and not the other becomes a scan
that silently reads the wrong field into the wrong destination.
*/
func (s *Store) GetAttachment(ctx context.Context, id uuid.UUID) (*AttachmentContent, error) {
	meta, err := s.GetAttachmentMeta(ctx, id)
	if err != nil {
		return nil, err
	}
	var content []byte
	if err := s.pool.QueryRow(ctx,
		`SELECT content FROM erp_attachments WHERE id = $1`, id).Scan(&content); err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &AttachmentContent{Attachment: *meta, Content: content}, nil
}

// GetAttachmentMeta reads one file's metadata without its bytes.
func (s *Store) GetAttachmentMeta(ctx context.Context, id uuid.UUID) (*Attachment, error) {
	return scanAttachment(s.pool.QueryRow(ctx,
		`SELECT `+attachmentColumns+` FROM erp_attachments WHERE id = $1`, id))
}

func (s *Store) DeleteAttachment(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM erp_attachments WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
