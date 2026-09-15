package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestParseOptionalUserID(t *testing.T) {
	id, err := parseOptionalUserID("")
	if err != nil || id != nil {
		t.Fatalf("empty: id=%v err=%v", id, err)
	}
	id, err = parseOptionalUserID("not-a-uuid")
	if err != ErrBadInput {
		t.Fatalf("invalid: err=%v", err)
	}
	id, err = parseOptionalUserID("550e8400-e29b-41d4-a716-446655440000")
	if err != nil || id == nil {
		t.Fatalf("valid uuid: id=%v err=%v", id, err)
	}
}

// hrModuleKeysMigration has to name the NEWEST migration that redefines the
// module CHECK, because that is the one in force once they have all run. A
// stale constant would point the drift test at a superseded constraint and the
// operator error at a migration that no longer carries the keys -- both would
// pass while the deployment disagreed with the build.
func TestHRModuleKeysMigrationIsTheNewestOne(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	newest := ""
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		body, err := os.ReadFile(filepath.Join("..", "..", "migrations", e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		// The runner applies files in lexical order, so the last one that
		// redefines the constraint is the one left standing.
		if strings.Contains(string(body), "CHECK (module IN (") && e.Name() > newest {
			newest = e.Name()
		}
	}
	if newest == "" {
		t.Fatal("no migration defines CHECK (module IN (...))")
	}
	if newest != hrModuleKeysMigration {
		t.Errorf("hrModuleKeysMigration is %q but the newest module CHECK is in %q",
			hrModuleKeysMigration, newest)
	}
}

// The Go allowlist and the database CHECK bound the same thing from two sides.
// Drift is silent in the direction that matters: a key IsHRModule accepts but
// the constraint rejects turns a clean 400 into a 500 on the first write.
func TestHRModuleKeysMatchMigrationConstraint(t *testing.T) {
	sql, err := os.ReadFile(filepath.Join("..", "..", "migrations", hrModuleKeysMigration))
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	body := string(sql)
	start := strings.LastIndex(body, "CHECK (module IN (")
	if start < 0 {
		t.Fatalf("%s has no CHECK (module IN (...))", hrModuleKeysMigration)
	}
	end := strings.Index(body[start:], "))")
	if end < 0 {
		t.Fatalf("unterminated CHECK in %s", hrModuleKeysMigration)
	}
	constraint := body[start : start+end]

	for _, key := range HRModuleKeys {
		if !strings.Contains(constraint, "'"+key+"'") {
			t.Errorf("HRModuleKeys accepts %q but the CHECK constraint rejects it", key)
		}
	}
	for _, quoted := range strings.Split(constraint, "'") {
		if quoted == "" || strings.ContainsAny(quoted, " (),\n\t-") && !IsHRModule(quoted) {
			continue
		}
		if strings.Contains(quoted, "CHECK") || strings.Contains(quoted, "module IN") {
			continue
		}
		if !IsHRModule(quoted) {
			t.Errorf("the CHECK constraint allows %q but IsHRModule rejects it", quoted)
		}
	}
}

// A CHECK violation on the module column is a pending migration, not bad input.
// Returning it verbatim hands an API client a Postgres error naming the table,
// the constraint and the SQLSTATE, and tells them nothing they can act on.
func TestHRModuleWriteErrClassifiesTheConstraintViolation(t *testing.T) {
	violation := &pgconn.PgError{
		Code:           "23514",
		ConstraintName: hrModuleCheckConstraint,
		Message:        `new row for relation "erp_hr_module_records" violates check constraint`,
	}
	got := hrModuleWriteErr(violation)
	if !errors.Is(got, ErrSchemaBehind) {
		t.Fatalf("module check violation should map to ErrSchemaBehind, got %v", got)
	}
	if !strings.Contains(got.Error(), hrModuleKeysMigration) {
		t.Errorf("the error should name the migration to apply: %v", got)
	}

	// A different constraint is a different problem and must pass through, or a
	// genuine data error would be reported as a deploy gap.
	other := &pgconn.PgError{Code: "23514", ConstraintName: "erp_employees_status_check"}
	if errors.Is(hrModuleWriteErr(other), ErrSchemaBehind) {
		t.Error("an unrelated check violation must not read as a schema gap")
	}
	if hrModuleWriteErr(nil) != nil {
		t.Error("nil must stay nil")
	}
}
