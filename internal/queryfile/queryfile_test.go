package queryfile

import (
	"strings"
	"testing"
)

const goodFile = `-- plinth: name: invoice-list
-- params: org_id:int:required | status:str:optional | limit:int:50
-- allow-tokens: web-bff, report-worker
-- semantics: dataset=invoices snapshot=2026-08-16a
-- timeout-ms: 3000
-- desc: 按机构列发票
SELECT id, buyer_id, status, amount_total, currency
FROM invoices
WHERE org_id = :org_id
  AND (:status::text IS NULL OR status = :status)
ORDER BY id DESC
LIMIT :limit
`

func TestParseGood(t *testing.T) {
	q, err := Parse("invoice-list.sql", goodFile)
	if err != nil {
		t.Fatal(err)
	}
	if q.Name != "invoice-list" || q.Mode != "read" || q.TimeoutMs != 3000 {
		t.Errorf("q = %+v", q)
	}
	if q.Desc != "按机构列发票" {
		t.Errorf("desc = %q", q.Desc)
	}
	if len(q.Params) != 3 {
		t.Fatalf("params = %+v", q.Params)
	}
	want := []Param{
		{Name: "org_id", Type: "int", Required: true},
		{Name: "status", Type: "str"},
		{Name: "limit", Type: "int", Default: "50"},
	}
	for i, w := range want {
		if q.Params[i] != w {
			t.Errorf("param[%d] = %+v, want %+v", i, q.Params[i], w)
		}
	}
	if len(q.AllowTokens) != 2 || q.AllowTokens[0] != "web-bff" || q.AllowTokens[1] != "report-worker" {
		t.Errorf("allowTokens = %v", q.AllowTokens)
	}
	if q.SemDataset != "invoices" || q.SemSnapshot != "2026-08-16a" {
		t.Errorf("semantics = %q %q", q.SemDataset, q.SemSnapshot)
	}
	if !strings.Contains(q.SQL, "SELECT id, buyer_id") || !strings.Contains(q.SQL, ":org_id") {
		t.Errorf("sql = %q", q.SQL)
	}
}

func TestParseMinimal(t *testing.T) {
	src := "-- plinth: name: q1\n-- allow-tokens: a\nSELECT 1 AS one\n"
	q, err := Parse("q1.sql", src)
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Params) != 0 || q.TimeoutMs != 0 || q.Mode != "read" || q.SemDataset != "" {
		t.Errorf("q = %+v", q)
	}
}

func TestParseErrors(t *testing.T) {
	// Each case pins the error message to its own validation: if that
	// validation is deleted, the substring check fails, not just err==nil.
	cases := map[string]struct {
		src string
		err string // substring the error must carry
	}{
		"missing name":      {"-- allow-tokens: a\nSELECT 1", "missing 'name'"},
		"name != file":      {"-- plinth: name: other\n-- allow-tokens: a\nSELECT 1", "must equal file base"},
		"no header":         {"SELECT 1", "missing header"},
		"missing tokens":    {"-- plinth: name: q\nSELECT 1", "allow-tokens"},
		"zero tokens":       {"-- plinth: name: q\n-- allow-tokens: , ,\nSELECT 1", "no tokens"},
		"write mode":        {"-- plinth: name: q\n-- allow-tokens: a\n-- mode: write\nSELECT 1", "reserved"},
		"dup key":           {"-- plinth: name: q\n-- plinth: name: q\n-- allow-tokens: a\nSELECT 1", "duplicate header key"},
		"bad param kind":    {"-- plinth: name: q\n-- allow-tokens: a\n-- params: x:bigint:required\nSELECT 1", "type must be"},
		"bad default":       {"-- plinth: name: q\n-- allow-tokens: a\n-- params: x:int:abc\nSELECT 1", "invalid syntax"},
		"empty param field": {"-- plinth: name: q\n-- allow-tokens: a\n-- params: x:int:\nSELECT 1", "empty required|optional|default"},
		"bad semantics":     {"-- plinth: name: q\n-- allow-tokens: a\n-- semantics: invoices\nSELECT 1", "semantics"},
		"empty sql":         {"-- plinth: name: q\n-- allow-tokens: a\n", "empty SQL body"},
		"bad timeout":       {"-- plinth: name: q\n-- allow-tokens: a\n-- timeout-ms: -5\nSELECT 1", "timeout-ms"},
		"malformed line":    {"-- plinth: name: q\n-- just a comment with no colon\n-- allow-tokens: a\nSELECT 1", "malformed header line"},
	}
	for name, tc := range cases {
		_, err := Parse("q.sql", tc.src)
		if err == nil {
			t.Errorf("%s: expected error", name)
			continue
		}
		if !strings.Contains(err.Error(), tc.err) {
			t.Errorf("%s: error %q does not identify cause %q", name, err, tc.err)
		}
	}
}

func TestAllows(t *testing.T) {
	q, err := Parse("q.sql", "-- plinth: name: q\n-- allow-tokens: web-bff, report-worker\nSELECT 1")
	if err != nil {
		t.Fatal(err)
	}
	if !q.Allows("web-bff") {
		t.Error("Allows(web-bff) = false, want true")
	}
	if q.Allows("worker") {
		t.Error("Allows(worker) = true, want false")
	}
	if q.Allows("") {
		t.Error(`Allows("") = true, want false`)
	}
}

func TestParseBOM(t *testing.T) {
	src := "\ufeff-- plinth: name: q1\n-- allow-tokens: a\nSELECT 1 AS one\n"
	if _, err := Parse("q1.sql", src); err != nil {
		t.Fatalf("BOM-prefixed file failed to parse: %v", err)
	}
}
