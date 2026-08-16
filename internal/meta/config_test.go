package meta

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	t.Setenv("TEST_DB_URL", "postgres://ro:pw@localhost:5432/silkline")
	t.Setenv("TEST_TOKEN", "s3cret")
	dir := t.TempDir()
	body := `database:
  url: ${TEST_DB_URL}
auth:
  tokens:
    web-bff: ${TEST_TOKEN}
engine:
  default_timeout_ms: 3000
  max_rows: 5000
audit:
  path: audit/executions.jsonl
  mask_params: [tax_id, buyer_name]
semantics:
  pull_command: lovrabet dataset export --format yaml
`
	if err := os.WriteFile(filepath.Join(dir, "plinth.yml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(filepath.Join(dir, "plinth.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.URL != "postgres://ro:pw@localhost:5432/silkline" {
		t.Errorf("url not expanded: %q", cfg.Database.URL)
	}
	if cfg.Auth.Tokens["web-bff"] != "s3cret" {
		t.Errorf("token not expanded: %v", cfg.Auth.Tokens)
	}
	if cfg.Engine.DefaultTimeoutMs != 3000 || cfg.Engine.MaxRows != 5000 {
		t.Errorf("engine = %+v", cfg.Engine)
	}
	if cfg.Audit.Path != "audit/executions.jsonl" || len(cfg.Audit.MaskParams) != 2 {
		t.Errorf("audit = %+v", cfg.Audit)
	}
	if cfg.Semantics.PullCommand != "lovrabet dataset export --format yaml" {
		t.Errorf("semantics = %+v", cfg.Semantics)
	}
}

func TestLoadConfigDefaultsAndErrors(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "plinth.yml")

	// minimal file → defaults
	if err := os.WriteFile(p, []byte("database:\n  url: postgres://x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Engine.DefaultTimeoutMs != 5000 || cfg.Engine.MaxRows != 10000 {
		t.Errorf("defaults = %+v", cfg.Engine)
	}
	if cfg.Audit.Path != "audit/executions.jsonl" {
		t.Errorf("audit default = %q", cfg.Audit.Path)
	}

	// missing file → error
	if _, err := LoadConfig(filepath.Join(dir, "nope.yml")); err == nil {
		t.Error("expected error for missing file")
	}
	// bad yaml → error
	if err := os.WriteFile(p, []byte("database: [unclosed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(p); err == nil {
		t.Error("expected error for bad yaml")
	}
}
