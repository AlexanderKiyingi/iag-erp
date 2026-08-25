package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

// The Go allowlist and the database CHECK bound the same thing from two sides.
// Drift is silent in the direction that matters: a key IsHRModule accepts but
// the constraint rejects turns a clean 400 into a 500 on the first write.
func TestHRModuleKeysMatchMigrationConstraint(t *testing.T) {
	sql, err := os.ReadFile(filepath.Join("..", "..", "migrations", "012_hr_module_keys.sql"))
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	body := string(sql)
	start := strings.LastIndex(body, "CHECK (module IN (")
	if start < 0 {
		t.Fatal("012_hr_module_keys.sql has no CHECK (module IN (...))")
	}
	end := strings.Index(body[start:], "))")
	if end < 0 {
		t.Fatal("unterminated CHECK in 012_hr_module_keys.sql")
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
