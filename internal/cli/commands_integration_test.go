//go:build integration

package cli

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dayuer/plinth/internal/registry"
	"github.com/dayuer/plinth/test/integration"
)

// writeProject lays out a minimal servable project: plinth.yml pointing at
// dbURL plus one allowed, param-typed query.
func writeProject(t *testing.T, dbURL string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "queries"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plinth.yml"), []byte("database:\n  url: "+dbURL+
		"\nauth:\n  tokens:\n    web-bff: tok1\n"+
		"audit:\n  path: "+filepath.Join(dir, "audit", "executions.jsonl")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "queries", "invoice-list.sql"), []byte(
		`-- plinth: name: invoice-list
-- params: org_id:int:required | status:str:optional
-- allow-tokens: web-bff
SELECT id, status FROM invoices WHERE org_id = :org_id AND (:status::text IS NULL OR status = :status) ORDER BY id
`), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestCLIValidateAndTest(t *testing.T) {
	pool := integration.StartPG(t)
	integration.ApplyFixtureOn(t, pool)
	dir := writeProject(t, pool.Config().ConnString())

	if err := Run([]string{"validate", "--dir", dir}); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if err := Run([]string{"test", "--query", "invoice-list", "--dir", dir, "--param", "org_id=1"}); err != nil {
		t.Fatalf("test: %v", err)
	}

	// a write query must fail validation with the metadata exit code
	if err := os.WriteFile(filepath.Join(dir, "queries", "bad.sql"), []byte(
		"-- plinth: name: bad\n-- allow-tokens: web-bff\nDELETE FROM invoices\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Run([]string{"validate", "--dir", dir})
	if err == nil || ExitCode(err) != 2 {
		t.Fatalf("expected exit 2, got %v", err)
	}
}

func TestSemanticsPullAndDrift(t *testing.T) {
	pool := integration.StartPG(t)
	integration.ApplyFixtureOn(t, pool)
	dir := writeProject(t, pool.Config().ConnString())

	script := filepath.Join(t.TempDir(), "fake-pull.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho 'datasets: invoices v1'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plinth.yml"), []byte("database:\n  url: "+pool.Config().ConnString()+
		"\nauth:\n  tokens:\n    web-bff: tok1\nsemantics:\n  pull_command: "+script+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Run([]string{"pull", "--dir", dir}); err != nil {
		t.Fatalf("pull: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "semantics", "datasets.yml")); err != nil {
		t.Fatal("semantics/datasets.yml not written")
	}
	snap, err := os.ReadFile(filepath.Join(dir, "semantics", "snapshot.txt"))
	if err != nil {
		t.Fatal("semantics/snapshot.txt not written")
	}
	if v := strings.TrimSpace(string(snap)); len(v) != 12 || strings.Trim(v, "0123456789abcdef") != "" {
		t.Fatalf("snapshot version %q is not 12 hex chars", v)
	}
	// no query pins a snapshot yet, so validation stays green
	if err := Run([]string{"validate", "--dir", dir}); err != nil {
		t.Fatalf("validate after pull: %v", err)
	}
}

// TestServeLifecycle exercises the exact stack `plinth serve` assembles
// (config → registry gate → pool → audit → engine → server) without
// blocking on a listener: buildStack is the shared construction path, so
// the tested order is the real one.
func TestServeLifecycle(t *testing.T) {
	pool := integration.StartPG(t)
	integration.ApplyFixtureOn(t, pool)
	dir := writeProject(t, pool.Config().ConnString())

	cfg, err := loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	reg, errs := registry.LoadDir(dir)
	if len(errs) > 0 || reg == nil {
		t.Fatalf("registry: %v", errs)
	}
	srv, shutdown, err := buildStack(cfg, reg)
	if err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	req, err := http.NewRequest("POST", ts.URL+"/q/invoice-list", strings.NewReader(`{"org_id":1}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Plinth-Token", "tok1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("POST /q/invoice-list = %d", resp.StatusCode)
	}

	// release pool + audit while the (request-idle) server is still up;
	// the audit line is already on disk
	if err := shutdown(); err != nil {
		t.Fatal(err)
	}
	aud, err := os.ReadFile(filepath.Join(dir, "audit", "executions.jsonl"))
	if err != nil {
		t.Fatal("audit file not written")
	}
	if !strings.Contains(string(aud), `"query":"invoice-list"`) {
		t.Fatalf("audit missing invoice-list line: %s", aud)
	}
}
