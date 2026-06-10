package store

import (
	"testing"
	"time"
)

func TestMatchesBirthday(t *testing.T) {
	birth := time.Date(1990, 6, 9, 0, 0, 0, 0, time.UTC)
	if !matchesBirthday(birth, time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)) {
		t.Fatal("expected match on month/day")
	}
	if matchesBirthday(birth, time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)) {
		t.Fatal("expected no match on different day")
	}
}

func TestCopyVars(t *testing.T) {
	src := map[string]string{"A": "1", "B": "2"}
	dst := copyVars(src)
	dst["A"] = "x"
	if src["A"] != "1" {
		t.Fatal("copyVars should clone map")
	}
}
