package registry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dayuer/plinth/internal/queryfile"
)

func writeQueries(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	qdir := filepath.Join(dir, "queries")
	if err := os.MkdirAll(qdir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(qdir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const q1 = `-- plinth: name: q1
-- allow-tokens: web-bff
SELECT id FROM invoices WHERE org_id = :org
`

// qgood is the same shape as q1 but with the name matching its file base
// (Parse enforces name == file base, so q1's body cannot live in good.sql).
const qgood = `-- plinth: name: good
-- allow-tokens: web-bff
SELECT id FROM invoices WHERE org_id = :org
`

func TestLoadDirOK(t *testing.T) {
	dir := writeQueries(t, map[string]string{"q1.sql": q1})
	reg, errs := LoadDir(dir)
	if len(errs) != 0 {
		t.Fatalf("errs = %v", errs)
	}
	if reg.Get("q1") == nil || !reg.Get("q1").Allows("web-bff") || reg.Get("q1").Allows("other") {
		t.Fatal("registry content wrong")
	}
	if len(reg.Names()) != 1 || reg.Names()[0] != "q1" {
		t.Fatalf("names = %v", reg.Names())
	}
	// non-.sql files ignored
	if err := os.WriteFile(filepath.Join(dir, "queries", "notes.txt"), []byte("junk"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg2, errs2 := LoadDir(dir)
	if len(errs2) != 0 || len(reg2.Names()) != 1 {
		t.Fatalf("non-sql files must be ignored: %v %v", reg2.Names(), errs2)
	}
}

func TestLoadDirMissingQueriesDir(t *testing.T) {
	dir := t.TempDir()
	_, errs := LoadDir(dir)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "queries/") {
		t.Fatalf("errs = %v", errs)
	}
}

func TestLoadDirCollectsAllErrors(t *testing.T) {
	dir := writeQueries(t, map[string]string{
		"bad1.sql": "-- plinth: name: bad1\n-- allow-tokens: a\nDELETE FROM invoices\n",
		"bad2.sql": "-- plinth: name: bad2\nSELECT 1\n", // missing allow-tokens
		"good.sql": qgood,
	})
	reg, errs := LoadDir(dir)
	if len(errs) != 2 {
		t.Fatalf("want 2 errors, got %v", errs)
	}
	for _, e := range errs {
		if !strings.Contains(e.Error(), "bad1") && !strings.Contains(e.Error(), "bad2") {
			t.Errorf("error lacks filename: %v", e)
		}
	}
	if reg.Get("good") == nil {
		t.Error("valid query must still load alongside broken ones")
	}
}

func TestLoadDirReadcheckError(t *testing.T) {
	dir := writeQueries(t, map[string]string{
		"evil.sql": "-- plinth: name: evil\n-- allow-tokens: a\nSELECT set_config('search_path','x',false)\n",
	})
	_, errs := LoadDir(dir)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "SET_CONFIG") {
		t.Fatalf("errs = %v", errs)
	}
}

// LoadDir cannot produce duplicate names through the filesystem (Parse
// enforces name == file base), so the guard is exercised through the
// internal add seam that LoadDir itself goes through.
func TestAddDuplicateName(t *testing.T) {
	reg := &Registry{queries: map[string]*queryfile.Query{}}
	first, err := queryfile.Parse("q1.sql", q1)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.add(first, "q1.sql"); err != nil {
		t.Fatalf("first add must succeed: %v", err)
	}
	err = reg.add(&queryfile.Query{Name: first.Name}, "again.sql")
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("want duplicate-name error, got %v", err)
	}
	if got := reg.Get("q1"); got != first {
		t.Fatal("duplicate must not overwrite the original")
	}
}

// LoadDir lists only top-level *.sql files: subdirectories are not walked.
func TestLoadDirIgnoresSubdirs(t *testing.T) {
	dir := writeQueries(t, map[string]string{"q1.sql": q1})
	sub := filepath.Join(dir, "queries", "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "q1.sql"), []byte(q1), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, errs := LoadDir(dir)
	if len(errs) != 0 {
		t.Fatalf("subdirectories must be ignored: %v", errs)
	}
	if len(reg.Names()) != 1 || reg.Names()[0] != "q1" {
		t.Fatalf("names = %v", reg.Names())
	}
}
