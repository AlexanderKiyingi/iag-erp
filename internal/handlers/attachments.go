/*
Attachment endpoints.

The upload body is the shape the IAG HR app already sends -- a list of files,
each carrying its bytes as a data URL -- so the app's change is the URL and
nothing else. Data URLs rather than multipart because that is what the browser
uploader was built around, and rewriting it to multipart would be a second
change to make while proving the first one works.

Downloads are served as attachments, never inline. A stored file is content
somebody else uploaded, and rendering it in the app's own origin would let an
HTML or SVG upload run as the person viewing it.
*/
package handlers

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"iag-erp/backend/internal/store"
)

/*
decodeDataURL pulls the bytes and the declared type out of a data: URL.

The declared type is returned but never trusted on its own -- the store checks
it against an allow-list, and the download re-serves it rather than sniffing.
A caller who lies about the type gets their file refused or served back as the
type they claimed, and in neither case does it become something executable.
*/
func decodeDataURL(raw string) (mime string, content []byte, err error) {
	if !strings.HasPrefix(raw, "data:") {
		return "", nil, fmt.Errorf("%w: file must be a data URL", store.ErrBadInput)
	}
	comma := strings.Index(raw, ",")
	if comma < 0 {
		return "", nil, fmt.Errorf("%w: malformed data URL", store.ErrBadInput)
	}
	header := raw[len("data:"):comma]
	body := raw[comma+1:]
	if !strings.Contains(header, ";base64") {
		return "", nil, fmt.Errorf("%w: data URL must be base64", store.ErrBadInput)
	}
	mime = strings.TrimSpace(strings.TrimSuffix(header, ";base64"))
	content, decodeErr := base64.StdEncoding.DecodeString(body)
	if decodeErr != nil {
		return "", nil, fmt.Errorf("%w: data URL is not valid base64", store.ErrBadInput)
	}
	return mime, content, nil
}

type attachmentUploadBody struct {
	RecordID string `json:"recordId"`
	Module   string `json:"module"`
	Entity   string `json:"entity"`
	Files    []struct {
		Name    string `json:"name"`
		MIME    string `json:"mime"`
		DataURL string `json:"dataUrl"`
	} `json:"files"`
}

func (a *API) UploadAttachments(c *gin.Context) {
	var body attachmentUploadBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(body.Files) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no files"})
		return
	}

	uploader := a.resolveCallerEmployeeNo(c)
	out := make([]gin.H, 0, len(body.Files))
	for _, f := range body.Files {
		mime, content, err := decodeDataURL(f.DataURL)
		if err != nil {
			writeStoreError(c, err)
			return
		}
		// The declared type on the file wins over the one in the data URL when
		// both are present: the uploader validated against the former.
		if strings.TrimSpace(f.MIME) != "" {
			mime = f.MIME
		}
		saved, err := a.Store.CreateAttachment(c.Request.Context(), store.CreateAttachmentInput{
			OwnerModule:   body.Module,
			OwnerEntity:   body.Entity,
			OwnerRecordID: body.RecordID,
			Filename:      f.Name,
			MIME:          mime,
			Content:       content,
			UploadedBy:    uploader,
		})
		if err != nil {
			// Stop at the first refusal rather than reporting a partial
			// success: the caller sends one file at a time, and a batch that
			// half-landed would leave them unable to say which.
			writeStoreError(c, err)
			return
		}
		out = append(out, gin.H{
			"storageId":  saved.ID.String(),
			"id":         saved.ID.String(),
			"name":       saved.Filename,
			"mime":       saved.MIME,
			"size":       saved.SizeBytes,
			"checksum":   saved.ChecksumSHA256,
			"uploadedAt": saved.CreatedAt,
		})
	}
	c.JSON(http.StatusCreated, gin.H{"ok": true, "data": out})
}

func (a *API) ListAttachments(c *gin.Context) {
	items, err := a.Store.ListAttachments(c.Request.Context(),
		c.Query("module"), c.Query("entity"), c.Query("record_id"))
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (a *API) DownloadAttachment(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	file, err := a.Store.GetAttachment(c.Request.Context(), id)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	// Always as an attachment, and always with nosniff. Between them these stop
	// a stored file being rendered as a document in this origin, which is the
	// difference between keeping a scan and hosting whatever was uploaded.
	c.Header("Content-Disposition",
		fmt.Sprintf("attachment; filename=%q", file.Filename))
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, file.MIME, file.Content)
}

func (a *API) DeleteAttachment(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := a.Store.DeleteAttachment(c.Request.Context(), id); err != nil {
		writeStoreError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
