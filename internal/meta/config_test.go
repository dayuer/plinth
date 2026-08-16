package meta

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	t.Setenv("TEST_DB_URL", "postgres://ro:pw@localhost:5432/silkline")
	t.Setenv("TEST_TOKEN", "s3cret")
	t.Setenv("TEST_TOKEN_2", "w0rk3r-tok")
	dir := t.TempDir()
	body := `database:
  url: ${TEST_DB_URL}
auth:
  tokens:
    web-bff: ${TEST_TOKEN}
    worker: ${TEST_TOKEN_2}
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
	if cfg.Auth.Tokens["worker"] != "w0rk3r-tok" {
		t.Errorf("second token not expanded: %v", cfg.Auth.Tokens)
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

func TestLoadConfigLiteralDollarPreserved(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "plinth.yml")
	body := "auth:\n  tokens:\n    legacy: a$2b$c\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Auth.Tokens["legacy"] != "a$2b$c" {
		t.Errorf("literal '$' must pass through untouched, got %q", cfg.Auth.Tokens["legacy"])
	}
}

func TestLoadConfigUnsetVarExpandsEmpty(t *testing.T) {
	os.Unsetenv("PLINTH_TEST_MISSING_VAR") // intentional: unset ${VAR} -> ""
	dir := t.TempDir()
	p := filepath.Join(dir, "plinth.yml")
	body := "auth:\n  tokens:\n    ghost: ${PLINTH_TEST_MISSING_VAR}\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Auth.Tokens["ghost"] != "" {
		t.Errorf("unset ${VAR} must expand to empty string, got %q", cfg.Auth.Tokens["ghost"])
	}
}

func TestLoadConfigRejectsUnknownKey(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "plinth.yml")
	body := "audit:\n  path: audit/executions.jsonl\n  mask_paramz: [tax_id]\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(p); err == nil {
		t.Error("expected error for unknown key mask_paramz")
	}
}

func TestLoadConfigRejectsDuplicateTokens(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "plinth.yml")

	// two callers sharing one literal token value → error
	body := "auth:\n  tokens:\n    web-bff: same-secret\n    worker: same-secret\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadConfig(p)
	if err == nil {
		t.Fatal("expected error for duplicate token value")
	}
	if !strings.Contains(err.Error(), "duplicate token value") {
		t.Errorf("error should name the duplicate, got %q", err)
	}

	// empty values count too: both ${UNSET} collide on "" → error
	os.Unsetenv("PLINTH_TEST_UNSET_A")
	os.Unsetenv("PLINTH_TEST_UNSET_B")
	body = "auth:\n  tokens:\n    web-bff: ${PLINTH_TEST_UNSET_A}\n    worker: ${PLINTH_TEST_UNSET_B}\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(p); err == nil {
		t.Error("expected error for duplicate empty token value")
	}
}
