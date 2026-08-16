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
	cases := map[string]string{
		"missing name":   "-- allow-tokens: a\nSELECT 1",
		"name != file":   "-- plinth: name: other\n-- allow-tokens: a\nSELECT 1",
		"no header":      "SELECT 1",
		"missing tokens": "-- plinth: name: q\nSELECT 1",
		"write mode":     "-- plinth: name: q\n-- allow-tokens: a\n-- mode: write\nSELECT 1",
		"dup key":        "-- plinth: name: q\n-- plinth: name: q\n-- allow-tokens: a\nSELECT 1",
		"bad param kind": "-- plinth: name: q\n-- allow-tokens: a\n-- params: x:bigint:required\nSELECT 1",
		"bad default":    "-- plinth: name: q\n-- allow-tokens: a\n-- params: x:int:abc\nSELECT 1",
		"bad semantics":  "-- plinth: name: q\n-- allow-tokens: a\n-- semantics: invoices\nSELECT 1",
		"empty sql":      "-- plinth: name: q\n-- allow-tokens: a\n",
		"bad timeout":    "-- plinth: name: q\n-- allow-tokens: a\n-- timeout-ms: -5\nSELECT 1",
		"malformed line": "-- plinth: name: q\n-- just a comment with no colon\n-- allow-tokens: a\nSELECT 1",
	}
	for name, src := range cases {
		if _, err := Parse("q.sql", src); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}
