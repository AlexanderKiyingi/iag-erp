package store

import "testing"

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
