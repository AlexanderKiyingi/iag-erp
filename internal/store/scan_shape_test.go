package store

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Column lists and their readers must agree.
//
// This guards a defect that has already happened once, in iag-finance: a SELECT
// gained a column, the three hand-written scans of that shape were not all
// updated, and two of them read thirteen columns into twelve destinations.
// Nothing failed at build time, because Scan is variadic and takes `...any` —
// the mismatch only surfaces when the query runs.
//
// The structural fix is one column list with one reader. This test enforces
// that: for every `<name>Columns` constant in the package, the number of
// columns it selects must equal the number of destinations its `scan<Name>`
// function passes to Scan.

var (
	columnConstRe = regexp.MustCompile("(?s)const (\\w+)Columns = `([^`]*)`")
	scanFuncRe    = regexp.MustCompile(`(?s)func scan(\w+)\(row pgx\.Row[^)]*\) \(\*\w+, error\) \{(.*?)\n\}`)
	scanCallRe    = regexp.MustCompile(`(?s)row\.Scan\((.*?)\)\s*\n`)
)

// countColumns counts comma-separated columns at paren depth zero, so a comma
// inside a subquery or an IN list is not mistaken for a column separator.
func countColumns(list string) int {
	depth, count := 0, 1
	for _, r := range list {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				count++
			}
		}
	}
	if strings.TrimSpace(list) == "" {
		return 0
	}
	return count
}

// countDestinations counts the `&x` arguments in a Scan call, ignoring commas
// inside any nested call.
func countDestinations(args string) int {
	depth, count := 0, 0
	for i, r := range args {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case '&':
			if depth == 0 {
				// Only count an & that begins an argument, not one inside an
				// expression.
				if i == 0 || strings.ContainsRune(" \t\n,", rune(args[i-1])) {
					count++
				}
			}
		}
	}
	return count
}

func packageSources(t *testing.T) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	out := map[string]string{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		body, err := os.ReadFile(filepath.Clean(name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		out[name] = string(body)
	}
	if len(out) == 0 {
		t.Fatal("no package sources found")
	}
	return out
}

func TestColumnListsMatchTheirScanners(t *testing.T) {
	sources := packageSources(t)

	// Collect every column constant and every scan function in the package.
	columns := map[string]int{}  // "Requisition" -> column count
	scanners := map[string]int{} // "Requisition" -> destination count
	scanFiles := map[string]string{}

	for file, src := range sources {
		for _, m := range columnConstRe.FindAllStringSubmatch(src, -1) {
			name := strings.Title(m[1]) //nolint:staticcheck // ASCII identifiers only
			columns[name] = countColumns(m[2])
		}
		for _, m := range scanFuncRe.FindAllStringSubmatch(src, -1) {
			call := scanCallRe.FindStringSubmatch(m[2])
			if call == nil {
				continue
			}
			scanners[m[1]] = countDestinations(call[1])
			scanFiles[m[1]] = file
		}
	}

	if len(columns) == 0 || len(scanners) == 0 {
		t.Fatalf("parsed %d column lists and %d scanners; the patterns have drifted "+
			"from the code and this test is no longer checking anything",
			len(columns), len(scanners))
	}

	checked := 0
	for name, cols := range columns {
		dests, ok := scanners[name]
		if !ok {
			// Not every column list has a same-named scanner (some are read by
			// a differently-named helper). Those are covered by the
			// single-reader test below.
			continue
		}
		checked++
		if cols != dests {
			t.Errorf("%s: %sColumns selects %d columns but scan%s passes %d destinations (%s)",
				name, strings.ToLower(name[:1])+name[1:], cols, name, dests, scanFiles[name])
		}
	}
	if checked == 0 {
		t.Fatal("matched no column list to a scanner; the test is vacuous")
	}
	t.Logf("checked %d column list/scanner pairs", checked)
}

// Each column list must have exactly one reader. Two hand-written scans of one
// shape is the condition that lets them drift; this fails if a second appears.
func TestEachColumnListHasOneReader(t *testing.T) {
	sources := packageSources(t)

	// A "wide" scan — eight or more destinations — is an entity read rather
	// than a leaf/child-table read, and is what must not be duplicated.
	const wideScan = 8

	type site struct{ file, snippet string }
	wide := []site{}
	scanAny := regexp.MustCompile(`(?s)(rows|row)\.Scan\((.*?)\)\s*;?\s*(err|;|\n)`)

	for file, src := range sources {
		for _, m := range scanAny.FindAllStringSubmatch(src, -1) {
			if countDestinations(m[2]) >= wideScan {
				first := strings.TrimSpace(strings.SplitN(strings.TrimSpace(m[2]), ",", 2)[0])
				wide = append(wide, site{file, first})
			}
		}
	}

	// Group by the first destination, which identifies the shape being read
	// (&e.ID for an employee, &p.ID for a payslip, and so on) together with its
	// file. Two wide scans of the same shape in the same file is the smell.
	seen := map[string][]string{}
	for _, s := range wide {
		key := s.file + " " + s.snippet
		seen[key] = append(seen[key], s.snippet)
	}
	for key, hits := range seen {
		if len(hits) > 1 {
			t.Errorf("%s has %d wide scans starting with the same destination — "+
				"one column list should have one reader, not %d hand-written copies",
				key, len(hits), len(hits))
		}
	}
}
