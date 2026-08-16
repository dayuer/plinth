//go:build integration

package server

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dayuer/plinth/internal/audit"
	"github.com/dayuer/plinth/internal/exec"
	"github.com/dayuer/plinth/internal/queryfile"
	"github.com/dayuer/plinth/internal/registry"
	"github.com/dayuer/plinth/test/integration"
)

func TestEndToEndAudit(t *testing.T) {
	pool := integration.StartPG(t)
	integration.ApplyFixtureOn(t, pool)
	q := &queryfile.Query{
		Name: "invoice-list", Mode: "read", AllowTokens: []string{"web-bff"},
		Params: []queryfile.Param{{Name: "org_id", Type: "int", Required: true}, {Name: "status", Type: "str"}},
		SQL: `SELECT id, status FROM invoices
WHERE org_id = :org_id AND (:status::text IS NULL OR status = :status)
ORDER BY id`,
	}
	audPath := filepath.Join(t.TempDir(), "exec.jsonl")
	aud, err := audit.Open(audPath, []string{"status"})
	if err != nil {
		t.Fatal(err)
	}
	defer aud.Close()
	s := New(registry.NewForTest(q),
		&exec.Engine{Pool: pool, DefaultTimeout: 5 * time.Second, MaxRows: 100},
		map[string]string{"web-bff": "tok1", "worker": "tok2"}, aud)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	req, _ := http.NewRequest("POST", ts.URL+"/q/invoice-list", strings.NewReader(`{"org_id":1,"status":"OPEN"}`))
	req.Header.Set("X-Plinth-Token", "tok1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var body struct {
		Rows []map[string]any `json:"rows"`
	}
	json.NewDecoder(resp.Body).Decode(&body)
	if len(body.Rows) != 1 || body.Rows[0]["status"] != "OPEN" {
		t.Fatalf("rows = %+v", body.Rows)
	}

	// A caller whose token authenticates but is not in allow-tokens gets
	// 403 — and that rejection must land in the audit log as denied.
	req2, _ := http.NewRequest("POST", ts.URL+"/q/invoice-list", strings.NewReader(`{"org_id":1}`))
	req2.Header.Set("X-Plinth-Token", "tok2")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != 403 {
		t.Fatalf("forbidden request = %d", resp2.StatusCode)
	}

	f, _ := os.Open(audPath)
	defer f.Close()
	sc := bufio.NewScanner(f)
	var recs []map[string]any
	for sc.Scan() {
		var m map[string]any
		json.Unmarshal(sc.Bytes(), &m)
		recs = append(recs, m)
	}
	if len(recs) != 2 {
		t.Fatalf("audit = %+v", recs)
	}
	if recs[0]["caller"] != "web-bff" || recs[0]["query"] != "invoice-list" {
		t.Fatalf("audit = %+v", recs)
	}
	if recs[0]["params"].(map[string]any)["status"] != "***" {
		t.Errorf("mask not applied: %v", recs[0]["params"])
	}
	if recs[0]["status"] != "ok" {
		t.Errorf("audit status = %v", recs[0]["status"])
	}
	d := recs[1]
	if d["caller"] != "worker" || d["query"] != "invoice-list" || d["status"] != "denied" {
		t.Errorf("denial audit = %+v", d)
	}
	if rows, _ := d["rows"].(float64); rows != 0 {
		t.Errorf("denial rows = %v, want 0", d["rows"])
	}
	if d["err"] != "forbidden" {
		t.Errorf("denial err = %v, want the problem title", d["err"])
	}
}
