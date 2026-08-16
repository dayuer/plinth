# Plinth Plan 1 v2: SQL BFF 地基 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 交付 v2 spec 的完整 MVP:`queries/*.sql` 命名查询注册中心 + 只读静态检查 + 命名参数执行引擎 + HTTP BFF(静态 token)+ JSONL 执行审计 + Lovrabet 语义快照拉取,CLI 五命令 `validate / test / pull / serve / status`。

**Architecture:** Go 单二进制多 package:`meta`(plinth.yml)、`queryfile`(注释头解析)、`sqlscan`(注释/字符串感知扫描)、`readcheck`(只读闸)、`registry`(加载校验)、`exec`(重写+执行)、`audit`(JSONL)、`server`(HTTP)、`cli`(接线)。安全双保险:加载时静态检查 + 运行时只读角色;参数永远绑定不拼接。

**Tech Stack:** Go 1.24+、`github.com/jackc/pgx/v5`、`gopkg.in/yaml.v3`、`github.com/testcontainers/testcontainers-go`(集成)、GitHub Actions。

**Spec:** `docs/specs/2026-08-16-plinth-design.md` v2.0。前置阅读:spec §3(文件格式)、§4(流水线)、§5(只读双保险)、§6(审计)。

---

### Task 1: 仓库脚手架与 CI

**Files:**
- Create: `go.mod`
- Create: `cmd/plinth/main.go`
- Create: `internal/cli/run.go`
- Create: `.github/workflows/ci.yml`
- Create: `Makefile`
- Create: `.gitignore`

- [ ] **Step 1: 初始化 module 与目录**

```bash
cd ~/sproot/plinth
go mod init github.com/dayuer/plinth
# 确认 go.mod 的 go 指令行 >= 1.24(server 用 mux "POST /q/{name}" 路由,1.22 起提供;集成测试用 t.Context(),1.24 起提供)
mkdir -p cmd/plinth internal/cli .github/workflows
```

- [ ] **Step 2: 写 main.go 与 CLI 骨架**

`cmd/plinth/main.go`:

```go
package main

import (
	"fmt"
	"os"

	"github.com/dayuer/plinth/internal/cli"
)

func main() {
	if err := cli.Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "plinth:", err)
		os.Exit(cli.ExitCode(err))
	}
}
```

`internal/cli/run.go`:

```go
package cli

import (
	"errors"
	"fmt"
)

// ExitCode maps errors to process exit codes (spec v2 §4):
// 2 = metadata/query definition invalid, 3 = database/runtime failure.
func ExitCode(err error) int {
	var metaErr *MetaError
	if errors.As(err, &metaErr) {
		return 2
	}
	var dbErr *DBError
	if errors.As(err, &dbErr) {
		return 3
	}
	return 1
}

type MetaError struct{ Err error }

func (e *MetaError) Error() string { return e.Err.Error() }
func (e *MetaError) Unwrap() error { return e.Err }

type DBError struct{ Err error }

func (e *DBError) Error() string { return e.Err.Error() }
func (e *DBError) Unwrap() error { return e.Err }

func usage() {
	fmt.Println(`plinth - AI-maintained SQL BFF for existing PostgreSQL

Usage:
  plinth validate [--dir DIR]            offline: load and check queries
  plinth test --query NAME [--dir DIR]   run one query against the database
  plinth semantics pull [--dir DIR]      refresh semantics snapshot
  plinth serve [--dir DIR] [--addr ADDR] start the HTTP BFF
  plinth status [--dir DIR]              query count + recent audit tail`)
}

// Run dispatches subcommands.
func Run(args []string) error {
	if len(args) == 0 {
		usage()
		return nil
	}
	switch args[0] {
	case "validate", "test", "pull", "serve", "status":
		return &MetaError{Err: fmt.Errorf("%s: not implemented yet (Plan 1 v2 in progress)", args[0])}
	default:
		usage()
		return fmt.Errorf("unknown command %q", args[0])
	}
}
```

- [ ] **Step 3: 写 CI、Makefile、.gitignore**

`.github/workflows/ci.yml`:

```yaml
name: ci
on: [push, pull_request]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.24"
      - name: gofmt
        run: test -z "$(gofmt -l .)" || (gofmt -l . && exit 1)
      - name: vet
        run: go vet ./...
      - name: test
        run: go test ./... -race -count=1
```

`Makefile`:

```make
.PHONY: build test lint
build:
	go build -o bin/plinth ./cmd/plinth
test:
	go test ./... -race -count=1
lint:
	go vet ./... && test -z "$$(gofmt -l .)"
```

`.gitignore`:

```
bin/
*.test
coverage.out
audit/
semantics/snapshot.txt
```

- [ ] **Step 4: 验证构建与 usage**

Run: `go build ./... && go run ./cmd/plinth`
Expected: 打印 usage,退出码 0

Run: `go run ./cmd/plinth validate; echo "exit=$?"`
Expected: stderr `plinth: validate: not implemented yet (Plan 1 v2 in progress)`,`exit=2`

- [ ] **Step 5: Commit**

```bash
git add go.mod cmd internal .github Makefile .gitignore
git commit -m "feat: module scaffold, CLI dispatch with exit-code mapping, CI"
```

### Task 2: meta 包——plinth.yml 配置加载

**Files:**
- Create: `internal/meta/config.go`
- Test: `internal/meta/config_test.go`

- [ ] **Step 1: 写失败测试**

`internal/meta/config_test.go`:

```go
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
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/meta/ -v`
Expected: FAIL(`undefined: LoadConfig`)

- [ ] **Step 3: 写实现**

`internal/meta/config.go`:

```go
// Package meta loads plinth.yml — the only non-query configuration.
package meta

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Database struct {
		URL string `yaml:"url"` // must be a read-only role
	} `yaml:"database"`
	Auth struct {
		Tokens map[string]string `yaml:"tokens"` // caller name -> token (env-expanded)
	} `yaml:"auth"`
	Engine struct {
		DefaultTimeoutMs int `yaml:"default_timeout_ms"`
		MaxRows          int `yaml:"max_rows"`
	} `yaml:"engine"`
	Audit struct {
		Path       string   `yaml:"path"`
		MaskParams []string `yaml:"mask_params"`
	} `yaml:"audit"`
	Semantics struct {
		PullCommand string `yaml:"pull_command"`
	} `yaml:"semantics"`
}

// LoadConfig reads plinth.yml and applies defaults:
// timeout 5000ms, max rows 10000, audit path audit/executions.jsonl.
// ${VAR} in database.url and token values is expanded from the environment.
func LoadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("plinth.yml: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("plinth.yml: %w", err)
	}
	cfg.Database.URL = os.ExpandEnv(cfg.Database.URL)
	for k, v := range cfg.Auth.Tokens {
		cfg.Auth.Tokens[k] = os.ExpandEnv(v)
	}
	if cfg.Engine.DefaultTimeoutMs <= 0 {
		cfg.Engine.DefaultTimeoutMs = 5000
	}
	if cfg.Engine.MaxRows <= 0 {
		cfg.Engine.MaxRows = 10000
	}
	if cfg.Audit.Path == "" {
		cfg.Audit.Path = "audit/executions.jsonl"
	}
	return &cfg, nil
}
```

- [ ] **Step 4: 拉依赖并跑测试确认通过**

Run: `go get gopkg.in/yaml.v3 && go test ./internal/meta/ -v`
Expected: 2 个测试 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/meta go.mod go.sum
git commit -m "feat(meta): plinth.yml config loader with env expansion and defaults"
```

### Task 3: queryfile 包——注释头解析

**Files:**
- Create: `internal/queryfile/queryfile.go`
- Test: `internal/queryfile/queryfile_test.go`

- [ ] **Step 1: 写失败测试**

`internal/queryfile/queryfile_test.go`:

```go
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
		"missing name":    "-- allow-tokens: a\nSELECT 1",
		"name != file":    "-- plinth: name: other\n-- allow-tokens: a\nSELECT 1",
		"no header":       "SELECT 1",
		"missing tokens":  "-- plinth: name: q\nSELECT 1",
		"write mode":      "-- plinth: name: q\n-- allow-tokens: a\n-- mode: write\nSELECT 1",
		"dup key":         "-- plinth: name: q\n-- plinth: name: q\n-- allow-tokens: a\nSELECT 1",
		"bad param kind":  "-- plinth: name: q\n-- allow-tokens: a\n-- params: x:bigint:required\nSELECT 1",
		"bad default":     "-- plinth: name: q\n-- allow-tokens: a\n-- params: x:int:abc\nSELECT 1",
		"bad semantics":   "-- plinth: name: q\n-- allow-tokens: a\n-- semantics: invoices\nSELECT 1",
		"empty sql":       "-- plinth: name: q\n-- allow-tokens: a\n",
		"bad timeout":     "-- plinth: name: q\n-- allow-tokens: a\n-- timeout-ms: -5\nSELECT 1",
		"malformed line":  "-- plinth: name: q\n-- just a comment with no colon\n-- allow-tokens: a\nSELECT 1",
	}
	for name, src := range cases {
		if _, err := Parse("q.sql", src); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/queryfile/ -v`
Expected: FAIL(`undefined: Parse`)

- [ ] **Step 3: 写实现**

> 修订(2026-08-16 执行中发现):头部首行为 `-- plinth: name: X`,其余键为裸 `-- key: value`;解析须在键值拆分前剥离可选的 `plinth:` 前缀,否则首行会整体键成 `header["plinth"]`。下方代码清单未含此剥离,以本修订为准。

`internal/queryfile/queryfile.go`:

```go
// Package queryfile parses queries/*.sql: a leading "-- key: value"
// comment header declaring metadata, then a :name-parameterized SQL body.
package queryfile

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

type Param struct {
	Name     string
	Type     string // int | str | bool | float
	Required bool
	Default  string // raw text, "" = none
}

type Query struct {
	Name        string
	Desc        string
	Params      []Param
	AllowTokens []string
	SemDataset  string
	SemSnapshot string
	Mode        string // always "read" in v2
	TimeoutMs   int    // 0 = engine default
	SQL         string
}

// Allows reports whether caller may invoke this query.
func (q *Query) Allows(caller string) bool {
	for _, c := range q.AllowTokens {
		if c == caller {
			return true
		}
	}
	return false
}

// Parse parses one query file. filename is used to enforce name == file base.
func Parse(filename string, src string) (*Query, error) {
	lines := strings.Split(src, "\n")
	header := map[string]string{}
	bodyStart := len(lines)
	for i, ln := range lines {
		t := strings.TrimSpace(ln)
		if !strings.HasPrefix(t, "-- ") {
			bodyStart = i
			break
		}
		rest := strings.TrimSpace(t[3:])
		colon := strings.Index(rest, ":")
		if colon <= 0 {
			return nil, fmt.Errorf("%s: malformed header line %d: %q", filename, i+1, ln)
		}
		k := strings.TrimSpace(rest[:colon])
		if _, dup := header[k]; dup {
			return nil, fmt.Errorf("%s: duplicate header key %q", filename, k)
		}
		header[k] = strings.TrimSpace(rest[colon+1:])
	}
	if len(header) == 0 {
		return nil, fmt.Errorf("%s: missing header; start the file with '-- plinth: name: ...' lines", filename)
	}

	q := &Query{Mode: "read"}
	q.Name = header["name"]
	q.Desc = header["desc"]
	if q.Name == "" {
		return nil, fmt.Errorf("%s: missing 'name' header", filename)
	}
	base := strings.TrimSuffix(filepath.Base(filename), ".sql")
	if q.Name != base {
		return nil, fmt.Errorf("%s: name %q must equal file base %q", filename, q.Name, base)
	}
	if header["allow-tokens"] == "" {
		return nil, fmt.Errorf("%s: missing 'allow-tokens' header", filename)
	}
	for _, tok := range strings.Split(header["allow-tokens"], ",") {
		if tok = strings.TrimSpace(tok); tok != "" {
			q.AllowTokens = append(q.AllowTokens, tok)
		}
	}
	if header["mode"] != "" {
		q.Mode = header["mode"]
	}
	if q.Mode != "read" {
		return nil, fmt.Errorf("%s: mode %q is reserved; only read queries load in v2", filename, q.Mode)
	}
	if header["semantics"] != "" {
		ds, snap, err := parseSemantics(header["semantics"])
		if err != nil {
			return nil, fmt.Errorf("%s: %v", filename, err)
		}
		q.SemDataset, q.SemSnapshot = ds, snap
	}
	if header["timeout-ms"] != "" {
		n, err := strconv.Atoi(header["timeout-ms"])
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("%s: timeout-ms must be a positive integer", filename)
		}
		q.TimeoutMs = n
	}
	if header["params"] != "" {
		seen := map[string]bool{}
		for _, part := range strings.Split(header["params"], "|") {
			p, err := parseParam(part)
			if err != nil {
				return nil, fmt.Errorf("%s: %v", filename, err)
			}
			if seen[p.Name] {
				return nil, fmt.Errorf("%s: duplicate param %q", filename, p.Name)
			}
			seen[p.Name] = true
			q.Params = append(q.Params, p)
		}
	}
	q.SQL = strings.TrimSpace(strings.Join(lines[bodyStart:], "\n"))
	if q.SQL == "" {
		return nil, fmt.Errorf("%s: empty SQL body", filename)
	}
	return q, nil
}

func parseParam(part string) (Param, error) {
	f := strings.Split(strings.TrimSpace(part), ":")
	if len(f) != 3 {
		return Param{}, fmt.Errorf("param %q: want name:type:required|optional|default", part)
	}
	p := Param{Name: strings.TrimSpace(f[0]), Type: strings.TrimSpace(f[1]), Default: strings.TrimSpace(f[2])}
	if !isIdent(p.Name) || p.Name == "" {
		return Param{}, fmt.Errorf("param %q: invalid name", part)
	}
	switch p.Type {
	case "int", "str", "bool", "float":
	default:
		return Param{}, fmt.Errorf("param %q: type must be int|str|bool|float", part)
	}
	switch {
	case p.Default == "required":
		p.Required = true
		p.Default = ""
	case p.Default == "optional":
		p.Default = ""
	default:
		if err := checkDefault(p.Type, p.Default); err != nil {
			return Param{}, fmt.Errorf("param %q: %v", part, err)
		}
	}
	return p, nil
}

func checkDefault(typ, val string) error {
	switch typ {
	case "int":
		_, err := strconv.Atoi(val)
		return err
	case "float":
		_, err := strconv.ParseFloat(val, 64)
		return err
	case "bool":
		_, err := strconv.ParseBool(val)
		return err
	}
	return nil // str: anything
}

func parseSemantics(v string) (dataset, snapshot string, err error) {
	for _, kv := range strings.Fields(v) {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 {
			return "", "", fmt.Errorf("semantics %q: want 'dataset=X snapshot=Y'", v)
		}
		switch parts[0] {
		case "dataset":
			dataset = parts[1]
		case "snapshot":
			snapshot = parts[1]
		default:
			return "", "", fmt.Errorf("semantics %q: unknown key %q", v, parts[0])
		}
	}
	if dataset == "" || snapshot == "" {
		return "", "", fmt.Errorf("semantics %q: both dataset and snapshot required", v)
	}
	return dataset, snapshot, nil
}

func isIdent(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (i > 0 && c >= '0' && c <= '9') {
			continue
		}
		return false
	}
	return len(s) > 0
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/queryfile/ -v`
Expected: 3 个测试全 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/queryfile
git commit -m "feat(queryfile): comment-header metadata parser for named queries"
```

### Task 4: sqlscan 包——注释/字符串感知扫描

**Files:**
- Create: `internal/sqlscan/sqlscan.go`
- Test: `internal/sqlscan/sqlscan_test.go`

单一状态机 `walk`,两个出口:`Analyze`(关键词扫描用,注释与字符串内容抹空、参数名抹空)与 `Rewrite`(:name → $N,其余原文保留)。

- [ ] **Step 1: 写失败测试**

`internal/sqlscan/sqlscan_test.go`:

```go
package sqlscan

import (
	"strings"
	"testing"
)

func TestAnalyzeBlanksCommentsAndStrings(t *testing.T) {
	src := "SELECT a -- delete from x\nFROM t WHERE s = 'insert into' AND n = /* update */ 1"
	clean, params, err := Analyze(src)
	if err != nil {
		t.Fatal(err)
	}
	upper := strings.ToUpper(clean)
	for _, kw := range []string{"DELETE", "INSERT", "UPDATE"} {
		if strings.Contains(upper, kw) {
			t.Errorf("keyword %s leaked into clean SQL: %q", kw, clean)
		}
	}
	if len(params) != 0 {
		t.Errorf("params = %v", params)
	}
	if !strings.Contains(strings.ToUpper(clean), "SELECT A") {
		t.Errorf("structure lost: %q", clean)
	}
}

func TestAnalyzeFindsParams(t *testing.T) {
	src := "SELECT * FROM t WHERE org = :org_id AND (:status::text IS NULL OR s = :status) LIMIT :limit"
	_, params, err := Analyze(src)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"org_id", "status", "limit"}
	if len(params) != len(want) {
		t.Fatalf("params = %v", params)
	}
	for i := range want {
		if params[i] != want[i] {
			t.Errorf("params[%d] = %s, want %s", i, params[i], want[i])
		}
	}
}

func TestAnalyzeIgnoresParamsInStringsAndComments(t *testing.T) {
	src := "SELECT ':fake' -- :also_fake\nFROM t WHERE x = :real"
	_, params, err := Analyze(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(params) != 1 || params[0] != "real" {
		t.Errorf("params = %v", params)
	}
}

func TestAnalyzeErrors(t *testing.T) {
	cases := []string{
		"SELECT 'unterminated",
		"SELECT $$unterminated",
		"SELECT E'back\\slash'",
		"SELECT 'a\\b'",
		"/* unterminated block",
	}
	for _, src := range cases {
		if _, _, err := Analyze(src); err == nil {
			t.Errorf("Analyze(%q): expected error", src)
		}
	}
}

func TestRewrite(t *testing.T) {
	src := "SELECT n -- keep me: intact\nFROM t WHERE a = :one AND b = :one AND c = :two AND d::text = 'x:literal'"
	out, order, err := Rewrite(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(order) != 2 || order[0] != "one" || order[1] != "two" {
		t.Fatalf("order = %v", order)
	}
	if !strings.Contains(out, "a = $1") || !strings.Contains(out, "b = $1") || !strings.Contains(out, "c = $2") {
		t.Errorf("out = %q", out)
	}
	if !strings.Contains(out, "d::text = 'x:literal'") {
		t.Errorf("cast/literal mangled: %q", out)
	}
	if !strings.Contains(out, "-- keep me: intact") {
		t.Errorf("comment mangled: %q", out)
	}
}

func TestDollarQuotesAndEscapedQuotes(t *testing.T) {
	src := "SELECT $$dollar :notparam$$, 'it''s :fine' WHERE x = :p"
	clean, params, err := Analyze(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(params) != 1 || params[0] != "p" {
		t.Errorf("params = %v", params)
	}
	if strings.Contains(clean, "notparam") || strings.Contains(clean, "fine") {
		t.Errorf("contents not blanked: %q", clean)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/sqlscan/ -v`
Expected: FAIL(`undefined: Analyze`)

- [ ] **Step 3: 写实现**

`internal/sqlscan/sqlscan.go`:

```go
// Package sqlscan walks SQL with full awareness of comments, string
// literals and casts. It powers read-only checking (Analyze) and named
// parameter rewriting (Rewrite).
package sqlscan

import (
	"fmt"
	"strings"
)

func isIdentChar(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// walk traverses sql. emit receives every non-parameter chunk verbatim
// (comments and literals included); param receives each :name occurrence
// and returns the text to substitute.
func walk(sql string, emit func(string), param func(name string) string) error {
	n := len(sql)
	i := 0
	for i < n {
		c := sql[i]
		switch {
		case c == '-' && i+1 < n && sql[i+1] == '-':
			j := i
			for j < n && sql[j] != '\n' {
				j++
			}
			emit(sql[i:j])
			i = j
		case c == '/' && i+1 < n && sql[i+1] == '*':
			j := i + 2
			for j < n && !(sql[j] == '*' && j+1 < n && sql[j+1] == '/') {
				j++
			}
			if j >= n {
				return fmt.Errorf("unterminated block comment")
			}
			emit(sql[i : j+2])
			i = j + 2
		case c == '\'':
			if i > 0 && (sql[i-1] == 'e' || sql[i-1] == 'E') && (i < 2 || !isIdentChar(sql[i-2])) {
				return fmt.Errorf("E'' string literals are not supported")
			}
			j := i + 1
			for j < n {
				if sql[j] == '\\' {
					return fmt.Errorf("backslash inside string literal is not supported")
				}
				if sql[j] == '\'' {
					if j+1 < n && sql[j+1] == '\'' {
						j += 2
						continue
					}
					break
				}
				j++
			}
			if j >= n {
				return fmt.Errorf("unterminated string literal")
			}
			emit(sql[i : j+1])
			i = j + 1
		case c == '$' && i+1 < n && sql[i+1] == '$':
			j := i + 2
			for j < n && !(sql[j] == '$' && j+1 < n && sql[j+1] == '$') {
				j++
			}
			if j >= n {
				return fmt.Errorf("unterminated dollar-quoted string")
			}
			emit(sql[i : j+2])
			i = j + 2
		case c == ':' && !(i+1 < n && sql[i+1] == ':'):
			j := i + 1
			for j < n && isIdentChar(sql[j]) {
				j++
			}
			if j == i+1 { // lone ':'
				emit(":")
				i++
				continue
			}
			emit(param(sql[i+1 : j]))
			i = j
		default:
			emit(string(c))
			i++
		}
	}
	return nil
}

// Analyze returns SQL safe for keyword scanning: every comment and string
// literal chunk has its contents blanked (newlines preserved), and :name
// parameters are blanked. Also returns distinct parameters in
// first-appearance order.
func Analyze(sql string) (clean string, params []string, err error) {
	seen := map[string]bool{}
	var b strings.Builder
	blank := func(s string) string {
		var sb strings.Builder
		for i := 0; i < len(s); i++ {
			if s[i] == '\n' {
				sb.WriteByte('\n')
			} else {
				sb.WriteByte(' ')
			}
		}
		return sb.String()
	}
	err = walk(sql,
		func(chunk string) {
			if len(chunk) == 1 {
				b.WriteByte(chunk[0])
				return
			}
			b.WriteString(blank(chunk))
		},
		func(name string) string {
			if !seen[name] {
				seen[name] = true
				params = append(params, name)
			}
			return ":" + strings.Repeat(" ", len(name))
		},
	)
	return b.String(), params, err
}

// Rewrite converts :name placeholders to $N in first-appearance order and
// returns the ordered parameter names. Comments and literals pass through
// untouched.
func Rewrite(sql string) (string, []string, error) {
	idx := map[string]int{}
	var order []string
	var b strings.Builder
	err := walk(sql,
		func(chunk string) { b.WriteString(chunk) },
		func(name string) string {
			if n, ok := idx[name]; ok {
				return fmt.Sprintf("$%d", n)
			}
			idx[name] = len(order) + 1
			order = append(order, name)
			return fmt.Sprintf("$%d", idx[name])
		},
	)
	return b.String(), order, err
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/sqlscan/ -v`
Expected: 6 个测试全 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/sqlscan
git commit -m "feat(sqlscan): comment/string-aware walker with Analyze and Rewrite"
```

### Task 5: readcheck 包——只读闸

**Files:**
- Create: `internal/readcheck/readcheck.go`
- Test: `internal/readcheck/readcheck_test.go`

- [ ] **Step 1: 写失败测试**

`internal/readcheck/readcheck_test.go`:

```go
package readcheck

import "testing"

func TestAcceptsReadonly(t *testing.T) {
	ok := []string{
		"SELECT 1",
		"select * from t where a = 1",
		"WITH x AS (SELECT 1) SELECT * FROM x",
		"SELECT count(*) FROM invoices GROUP BY org_id",
		"SELECT 'insert into is just data' FROM t",
		"SELECT * FROM t -- comment says update",
		"SELECT * FROM t WHERE note = $$delete me$$",
	}
	for _, sql := range ok {
		if err := Check(sql); err != nil {
			t.Errorf("Check(%q) = %v, want nil", sql, err)
		}
	}
}

func TestRejectsWritesAndDanger(t *testing.T) {
	bad := []string{
		"SELECT 1; DROP TABLE t",                       // multi statement
		"WITH d AS (DELETE FROM t RETURNING *) SELECT * FROM d", // CTE write
		"SELECT * INTO newtable FROM t",                // INTO
		"INSERT INTO t VALUES (1)",
		"UPDATE t SET a = 1",
		"DELETE FROM t",
		"CREATE TABLE x (id int)",
		"ALTER TABLE t ADD COLUMN x int",
		"DROP TABLE t",
		"TRUNCATE t",
		"GRANT SELECT ON t TO someone",
		"COPY t FROM '/etc/passwd'",
		"CALL some_proc()",
		"SELECT pg_sleep(10)",
		"SELECT pg_read_file('postgresql.conf')",
		"SELECT lo_import('/etc/passwd')",
		"SELECT 1; ",                                  // trailing semicolon
	}
	for _, sql := range bad {
		if err := Check(sql); err == nil {
			t.Errorf("Check(%q) = nil, want error", sql)
		}
	}
}

func TestRejectsBrokenLexing(t *testing.T) {
	for _, sql := range []string{"SELECT 'unterminated", "SELECT E'x\\'"} {
		if err := Check(sql); err == nil {
			t.Errorf("Check(%q) = nil, want error", sql)
		}
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/readcheck/ -v`
Expected: FAIL(`undefined: Check`)

- [ ] **Step 3: 写实现**

`internal/readcheck/readcheck.go`:

```go
// Package readcheck is the load-time read-only gate (spec v2 §5).
// It scans sqlscan-blanked SQL: must start with SELECT/WITH, contain no
// ';', and contain no forbidden keyword. The read-only DB role is the
// independent runtime backstop.
package readcheck

import (
	"fmt"
	"strings"

	"github.com/dayuer/plinth/internal/sqlscan"
)

var forbidden = []string{
	"INSERT", "UPDATE", "DELETE", "MERGE", "CREATE", "ALTER", "DROP",
	"TRUNCATE", "GRANT", "REVOKE", "COPY", "CALL", "DO", "SET", "RESET",
	"VACUUM", "REINDEX", "INTO", "LOCK", "LISTEN", "NOTIFY", "PREPARE",
	"EXECUTE", "DISCARD", "IMPORT", "LOAD", "CHECKPOINT", "REASSIGN",
	"PG_READ_FILE", "PG_READ_BINARY_FILE", "PG_LS_DIR", "PG_STAT_FILE",
	"PG_SLEEP", "PG_TERMINATE_BACKEND", "PG_CANCEL_BACKEND",
	"LO_IMPORT", "LO_EXPORT",
}

func Check(sql string) error {
	clean, _, err := sqlscan.Analyze(sql)
	if err != nil {
		return err
	}
	trimmed := strings.TrimSpace(clean)
	upper := strings.ToUpper(trimmed)
	if !strings.HasPrefix(upper, "SELECT") && !strings.HasPrefix(upper, "WITH") {
		return fmt.Errorf("query must start with SELECT or WITH")
	}
	if strings.Contains(clean, ";") {
		return fmt.Errorf("multiple statements are not allowed")
	}
	bad := map[string]bool{}
	for _, w := range forbidden {
		bad[w] = true
	}
	for _, w := range words(upper) {
		if bad[w] {
			return fmt.Errorf("forbidden keyword %s", w)
		}
	}
	return nil
}

func words(s string) []string {
	var out []string
	var cur strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '_' || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			cur.WriteByte(c)
		} else if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/readcheck/ -v`
Expected: 3 个测试全 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/readcheck
git commit -m "feat(readcheck): read-only gate — keyword scan over blanked SQL"
```

### Task 6: registry 包——目录加载与全量校验

**Files:**
- Create: `internal/registry/registry.go`
- Test: `internal/registry/registry_test.go`

- [ ] **Step 1: 写失败测试**

`internal/registry/registry_test.go`:

```go
package registry

import (
	"os"
	"path/filepath"
	"testing"
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

func TestLoadDirOK(t *testing.T) {
	dir := writeQueries(t, map[string]string{"q1.sql": q1})
	reg, errs := LoadDir(dir)
	if len(errs) != 0 {
		t.Fatalf("errs = %v", errs)
	}
	if reg.Get("q1") == nil || !reg.Get("q1").Allows("web-bff") || reg.Get("q1").Allows("other") {
		t.Fatal("registry content wrong")
	}
}

func TestLoadDirReadcheckApplies(t *testing.T) {
	bad := "-- plinth: name: bad\n-- allow-tokens: a\nDELETE FROM invoices\n"
	dir := writeQueries(t, map[string]string{"bad.sql": bad})
	_, errs := LoadDir(dir)
	if len(errs) == 0 {
		t.Fatal("expected readcheck error")
	}
}

func TestLoadDirDuplicateName(t *testing.T) {
	// same declared name in two files is impossible when name==filebase,
	// but two files q1.sql in different case still collide on Get; test
	// the collision guard via symlink-free duplicate content.
	dir := writeQueries(t, map[string]string{"q1.sql": q1})
	// simulate second file with same name by another dir entry
	if err := os.WriteFile(filepath.Join(dir, "queries", "q1.sql"), []byte(q1), 0o644); err != nil {
		t.Fatal(err)
	}
	_, errs := LoadDir(dir)
	if len(errs) != 0 {
		t.Fatalf("rewriting same file should not error: %v", errs)
	}
}

func TestLoadDirParamConsistencyCheckedAtExec(t *testing.T) {
	// declared params that do not appear in SQL are fine at load time
	// (exec catches mismatch); load only checks structure + readcheck.
	q := "-- plinth: name: q2\n-- allow-tokens: a\n-- params: x:int:required\nSELECT 1\n"
	dir := writeQueries(t, map[string]string{"q2.sql": q})
	_, errs := LoadDir(dir)
	if len(errs) != 0 {
		t.Fatalf("errs = %v", errs)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/registry/ -v`
Expected: FAIL(`undefined: LoadDir`)

- [ ] **Step 3: 写实现**

`internal/registry/registry.go`:

```go
// Package registry loads queries/ and enforces load-time gates:
// queryfile syntax, read-only check, name uniqueness.
package registry

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dayuer/plinth/internal/queryfile"
	"github.com/dayuer/plinth/internal/readcheck"
)

type Registry struct {
	queries map[string]*queryfile.Query
}

func (r *Registry) Get(name string) *queryfile.Query {
	return r.queries[name]
}

func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.queries))
	for k := range r.queries {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// LoadDir reads DIR/queries/*.sql. It returns all problems (never stops
// at the first) so validate prints a full report.
func LoadDir(dir string) (*Registry, []error) {
	qdir := filepath.Join(dir, "queries")
	entries, err := os.ReadDir(qdir)
	if os.IsNotExist(err) {
		return &Registry{queries: map[string]*queryfile.Query{}},
			[]error{fmt.Errorf("queries/: directory not found under %s", dir)}
	}
	if err != nil {
		return nil, []error{err}
	}
	var errs []error
	reg := &Registry{queries: map[string]*queryfile.Query{}}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		path := filepath.Join(qdir, e.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		q, err := queryfile.Parse(e.Name(), string(b))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if err := readcheck.Check(q.SQL); err != nil {
			errs = append(errs, fmt.Errorf("%s: %v", e.Name(), err))
			continue
		}
		if _, dup := reg.queries[q.Name]; dup {
			errs = append(errs, fmt.Errorf("%s: duplicate query name %q", e.Name(), q.Name))
			continue
		}
		reg.queries[q.Name] = q
	}
	return reg, errs
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/registry/ -v`
Expected: 4 个测试 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/registry
git commit -m "feat(registry): load queries/ with readcheck gate"
```

### Task 7: exec 包——参数强转、重写与执行(集成)

**Files:**
- Create: `internal/exec/exec.go`
- Create: `internal/exec/coerce.go`
- Create: `test/integration/pg.go`
- Create: `test/integration/fixture.go`
- Test: `internal/exec/exec_test.go`
- Test: `internal/exec/exec_integration_test.go`

- [ ] **Step 1: 写失败单元测试(强转)**

`internal/exec/exec_test.go`:

```go
package exec

import (
	"testing"

	"github.com/dayuer/plinth/internal/queryfile"
)

func ps(ps2 ...queryfile.Param) []queryfile.Param { return ps2 }

func TestCoerce(t *testing.T) {
	params := ps(
		queryfile.Param{Name: "org", Type: "int", Required: true},
		queryfile.Param{Name: "status", Type: "str"},
		queryfile.Param{Name: "limit", Type: "int", Default: "50"},
		queryfile.Param{Name: "flag", Type: "bool"},
		queryfile.Param{Name: "ratio", Type: "float"},
	)

	got, err := Coerce(params, map[string]any{"org": float64(42), "status": "OPEN", "flag": true, "ratio": 0.5})
	if err != nil {
		t.Fatal(err)
	}
	if got["org"] != int64(42) || got["status"] != "OPEN" || got["flag"] != true || got["ratio"] != 0.5 {
		t.Errorf("got = %v", got)
	}
	if got["limit"] != int64(50) {
		t.Errorf("default not applied: %v", got["limit"])
	}
}

func TestCoerceOptionalNullAndErrors(t *testing.T) {
	params := ps(queryfile.Param{Name: "s", Type: "str"})
	got, err := Coerce(params, nil)
	if err != nil || got["s"] != nil {
		t.Errorf("optional no default should bind NULL: %v %v", got, err)
	}

	if _, err := Coerce(ps(queryfile.Param{Name: "o", Type: "int", Required: true}), nil); err == nil {
		t.Error("missing required should error")
	}
	if _, err := Coerce(ps(queryfile.Param{Name: "o", Type: "int", Required: true}), map[string]any{"o": "x"}); err == nil {
		t.Error("str for int should error")
	}
	if _, err := Coerce(ps(queryfile.Param{Name: "o", Type: "int", Required: true}), map[string]any{"o": 1.5}); err == nil {
		t.Error("fractional for int should error")
	}
	if _, err := Coerce(ps(queryfile.Param{Name: "o", Type: "int", Required: true}), map[string]any{"o": 1, "ghost": 2}); err == nil {
		t.Error("unknown param should error")
	}
}
```

- [ ] **Step 2: 写集成 harness 与 fixture**

`test/integration/pg.go` 与 fixture 内容同 Plan 1 v1(Task 6 v1):`StartPG`(testcontainers postgres:16-alpine,无 Docker 则 Skip)、`ApplyFixture`(执行 `fixtureSQL`)。fixture 增加只读角色:

`test/integration/fixture.go`:

```go
package integration

// fixtureSQL creates a minimal SilkLine-shaped schema plus a read-only role.
const fixtureSQL = `
CREATE TABLE organizations (
  id   bigint PRIMARY KEY,
  name text NOT NULL
);
CREATE TABLE buyers (
  id     bigint PRIMARY KEY,
  org_id bigint NOT NULL REFERENCES organizations(id),
  name   text NOT NULL
);
CREATE TABLE invoices (
  id            bigserial PRIMARY KEY,
  org_id        bigint NOT NULL REFERENCES organizations(id),
  buyer_id      bigint REFERENCES buyers(id),
  status        text NOT NULL,
  amount_total  numeric(14,2),
  currency      text,
  internal_note text,
  active        boolean NOT NULL DEFAULT true,
  deleted_at    timestamptz
);
CREATE ROLE plinth_ro LOGIN PASSWORD 'ro_pass';
GRANT USAGE ON SCHEMA public TO plinth_ro;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO plinth_ro;
INSERT INTO organizations (id, name) VALUES (1, 'Org One'), (2, 'Org Two');
INSERT INTO invoices (org_id, status, amount_total, currency) VALUES
  (1, 'OPEN',   100.00, 'IDR'),
  (1, 'PAID',   250.00, 'IDR'),
  (2, 'OPEN',    50.00, 'USD');
`
```

`StartPG`、`ApplyFixture` 代码照 Plan 1 v1 Task 6 Step 1 的 `test/integration/pg.go` 原样写入(`StartPG` 返回 `*pgxpool.Pool`,`ApplyFixture(t, pool)` 执行 `fixtureSQL`)。

- [ ] **Step 3: 写失败集成测试**

`internal/exec/exec_integration_test.go`:

```go
//go:build integration

package exec

import (
	"strings"
	"testing"
	"time"

	"github.com/dayuer/plinth/internal/queryfile"
	"github.com/dayuer/plinth/test/integration"
)

func engine(t *testing.T) (*Engine, *integration.FixtureHandle) {
	t.Helper()
	pool := integration.StartPG(t)
	integration.ApplyFixture(t, pool)
	return &Engine{Pool: pool, DefaultTimeout: 5 * time.Second, MaxRows: 100}, nil
}

const invoiceListSQL = `SELECT id, org_id, status, amount_total, currency
FROM invoices
WHERE org_id = :org_id
  AND (:status::text IS NULL OR status = :status)
ORDER BY id DESC
LIMIT :limit`

func TestRunNamedParams(t *testing.T) {
	eng, _ := engine(t)
	q := &queryfile.Query{Name: "t", Mode: "read", SQL: invoiceListSQL,
		Params: []queryfile.Param{
			{Name: "org_id", Type: "int", Required: true},
			{Name: "status", Type: "str"},
			{Name: "limit", Type: "int", Default: "10"},
		}}
	res, err := eng.Run(t.Context(), q, map[string]any{"org_id": float64(1)})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 2 {
		t.Fatalf("rows = %d, want 2: %+v", len(res.Rows), res.Rows)
	}
	if res.Rows[0]["currency"] != "IDR" {
		t.Errorf("row0 = %+v", res.Rows[0])
	}

	res, err = eng.Run(t.Context(), q, map[string]any{"org_id": float64(1), "status": "PAID"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 1 || res.Rows[0]["status"] != "PAID" {
		t.Errorf("filtered rows = %+v", res.Rows)
	}
}

func TestRunParamMismatch(t *testing.T) {
	eng, _ := engine(t)
	q := &queryfile.Query{Name: "t", Mode: "read", SQL: "SELECT id FROM invoices WHERE org_id = :org_id",
		Params: []queryfile.Param{{Name: "org_id", Type: "int", Required: true}, {Name: "ghost", Type: "str"}}}
	if _, err := eng.Run(t.Context(), q, map[string]any{"org_id": 1.0}); err == nil {
		t.Fatal("declared-but-unused param should error")
	} else if !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunRowCap(t *testing.T) {
	eng, _ := engine(t)
	q := &queryfile.Query{Name: "t", Mode: "read", SQL: "SELECT id FROM invoices",
		Params: []queryfile.Param{}}
	eng.MaxRows = 2
	if _, err := eng.Run(t.Context(), q, nil); err == nil {
		t.Fatal("row cap should error when exceeded")
	}
}
```

(`integration.FixtureHandle` 不需要则省略该返回值;此处简化为只返回 engine。)

- [ ] **Step 4: 跑测试确认失败**

Run: `go test ./internal/exec/ -v && go test -tags integration ./internal/exec/ -v`
Expected: FAIL(`undefined: Coerce/Engine`)

- [ ] **Step 5: 写实现**

`internal/exec/coerce.go`:

```go
package exec

import (
	"fmt"

	"github.com/dayuer/plinth/internal/queryfile"
)

// Coerce validates and converts JSON-decoded input (map[string]any) into
// typed bind values, applying defaults. Optional params without default
// bind NULL.
func Coerce(ps []queryfile.Param, in map[string]any) (map[string]any, error) {
	declared := map[string]bool{}
	for _, p := range ps {
		declared[p.Name] = true
	}
	for k := range in {
		if !declared[k] {
			return nil, fmt.Errorf("unknown parameter %q", k)
		}
	}
	out := map[string]any{}
	for _, p := range ps {
		v, ok := in[p.Name]
		if !ok {
			if p.Required {
				return nil, fmt.Errorf("missing required parameter %q", p.Name)
			}
			if p.Default == "" {
				out[p.Name] = nil
				continue
			}
			d, err := defaultValue(p.Type, p.Default)
			if err != nil {
				return nil, fmt.Errorf("parameter %q default: %v", p.Name, err)
			}
			out[p.Name] = d
			continue
		}
		cv, err := coerceValue(p.Type, v)
		if err != nil {
			return nil, fmt.Errorf("parameter %q: %v", p.Name, err)
		}
		out[p.Name] = cv
	}
	return out, nil
}

func defaultValue(typ, raw string) (any, error) {
	switch typ {
	case "int":
		return parse(raw, func(s string) (any, error) { return atoi64(s) })
	case "float":
		return parse(raw, func(s string) (any, error) { return atof(s) })
	case "bool":
		return parse(raw, func(s string) (any, error) { return atob(s) })
	}
	return raw, nil // str
}

func coerceValue(typ string, v any) (any, error) {
	switch typ {
	case "int":
		switch n := v.(type) {
		case float64:
			if n != float64(int64(n)) {
				return nil, fmt.Errorf("want integer, got %v", n)
			}
			return int64(n), nil
		case int64:
			return n, nil
		default:
			return nil, fmt.Errorf("want integer, got %T", v)
		}
	case "float":
		if f, ok := v.(float64); ok {
			return f, nil
		}
		return nil, fmt.Errorf("want number, got %T", v)
	case "bool":
		if b, ok := v.(bool); ok {
			return b, nil
		}
		return nil, fmt.Errorf("want boolean, got %T", v)
	case "str":
		if s, ok := v.(string); ok {
			return s, nil
		}
		return nil, fmt.Errorf("want string, got %T", v)
	}
	return nil, fmt.Errorf("unknown type %q", typ)
}
```

小工具 `atoi64/atof/atob/parse` 放同文件(strconv 包装,parse 把错误文本统一)。实现时直接用 `strconv.ParseInt/ParseFloat/ParseBool`。

`internal/exec/exec.go`:

```go
// Package exec rewrites :name parameters to $N and runs queries through a
// read-only pool with timeout and row cap.
package exec

import (
	"context"
	"fmt"
	"time"

	"github.com/dayuer/plinth/internal/queryfile"
	"github.com/dayuer/plinth/internal/sqlscan"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Engine struct {
	Pool           *pgxpool.Pool
	DefaultTimeout time.Duration
	MaxRows        int
}

type Result struct {
	Rows       []map[string]any
	DurationMs int64
}

func (e *Engine) Run(ctx context.Context, q *queryfile.Query, in map[string]any) (*Result, error) {
	args, err := Coerce(q.Params, in)
	if err != nil {
		return nil, err
	}
	sqlText, order, err := sqlscan.Rewrite(q.SQL)
	if err != nil {
		return nil, err
	}
	if err := matchParams(q.Params, order); err != nil {
		return nil, err
	}
	timeout := e.DefaultTimeout
	if q.TimeoutMs > 0 {
		timeout = time.Duration(q.TimeoutMs) * time.Millisecond
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	rows, err := e.Pool.Query(cctx, sqlText, bind(order, args)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	res := &Result{Rows: []map[string]any{}}
	fd := rows.FieldDescriptions()
	for rows.Next() {
		if len(res.Rows) >= e.MaxRows {
			return nil, fmt.Errorf("row limit %d exceeded", e.MaxRows)
		}
		vals, err := rows.Values()
		if err != nil {
			return nil, err
		}
		row := map[string]any{}
		for i := range fd {
			row[string(fd[i].Name)] = vals[i]
		}
		res.Rows = append(res.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	res.DurationMs = time.Since(start).Milliseconds()
	return res, nil
}

// matchParams requires declared params and SQL params to be exactly equal.
func matchParams(ps []queryfile.Param, order []string) error {
	declared := map[string]bool{}
	for _, p := range ps {
		declared[p.Name] = true
	}
	seen := map[string]bool{}
	for _, n := range order {
		if !declared[n] {
			return fmt.Errorf("SQL uses undeclared parameter %q", n)
		}
		seen[n] = true
	}
	for _, p := range ps {
		if !seen[p.Name] {
			return fmt.Errorf("declared parameter %q does not appear in SQL", p.Name)
		}
	}
	return nil
}

func bind(order []string, args map[string]any) []any {
	out := make([]any, len(order))
	for i, name := range order {
		out[i] = args[name]
	}
	return out
}
```

- [ ] **Step 6: 拉依赖并跑测试确认通过**

Run: `go get github.com/jackc/pgx/v5 github.com/testcontainers/testcontainers-go/modules/postgres && go test ./internal/exec/ -v && go test -tags integration ./internal/exec/ -v`
Expected: 单元 + 集成全 PASS(无 Docker SKIP)

- [ ] **Step 7: Commit**

```bash
git add internal/exec test go.mod go.sum
git commit -m "feat(exec): named-param rewrite, typed coercion, capped read-only execution"
```

### Task 8: audit 包——JSONL 执行审计与脱敏

**Files:**
- Create: `internal/audit/audit.go`
- Test: `internal/audit/audit_test.go`

- [ ] **Step 1: 写失败测试**

`internal/audit/audit_test.go`:

```go
package audit

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecordAndMask(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit", "executions.jsonl")
	w, err := Open(path, []string{"tax_id", "buyer_name"})
	if err != nil {
		t.Fatal(err)
	}
	rec := Record{
		TS: time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC),
		Caller: "web-bff", Query: "invoice-list",
		Params: map[string]any{"org_id": float64(1), "tax_id": "NPWP123", "buyer_name": "Acme"},
		Rows: 3, Ms: 12, Status: "ok",
	}
	if err := w.Record(rec); err != nil {
		t.Fatal(err)
	}
	rec.Err, rec.Status = "boom", "error"
	rec.Params = map[string]any{"tax_id": "NPWP456"}
	_ = w.Record(rec)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	var lines []map[string]any
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, m)
	}
	if len(lines) != 2 {
		t.Fatalf("lines = %d", len(lines))
	}
	if lines[0]["params"].(map[string]any)["tax_id"] != "***" {
		t.Errorf("tax_id not masked: %v", lines[0]["params"])
	}
	if lines[0]["params"].(map[string]any)["org_id"] != float64(1) {
		t.Errorf("org_id mangled: %v", lines[0]["params"])
	}
	if lines[1]["status"] != "error" || lines[1]["err"] != "boom" {
		t.Errorf("error record = %v", lines[1])
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/audit/ -v`
Expected: FAIL(`undefined: Open`)

- [ ] **Step 3: 写实现**

`internal/audit/audit.go`:

```go
// Package audit appends execution records to a JSONL file. Masked
// parameters are recorded as "***". Write failures are returned to the
// caller (server logs and continues — spec v2 §6).
package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Record struct {
	TS     time.Time      `json:"ts"`
	Caller string         `json:"caller"`
	Query  string         `json:"query"`
	Params map[string]any `json:"params,omitempty"`
	Rows   int            `json:"rows"`
	Ms     int64          `json:"ms"`
	Status string         `json:"status"` // ok | error
	Err    string         `json:"err,omitempty"`
}

type Writer struct {
	mu   sync.Mutex
	f    *os.File
	mask map[string]bool
}

func Open(path string, maskParams []string) (*Writer, error) {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	m := map[string]bool{}
	for _, p := range maskParams {
		m[p] = true
	}
	return &Writer{f: f, mask: m}, nil
}

func (w *Writer) Record(r Record) error {
	if r.Params != nil {
		p := make(map[string]any, len(r.Params))
		for k, v := range r.Params {
			if w.mask[k] {
				p[k] = "***"
			} else {
				p[k] = v
			}
		}
		r.Params = p
	}
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	_, err = w.f.Write(append(b, '\n'))
	return err
}

func (w *Writer) Close() error { return w.f.Close() }
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/audit/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/audit
git commit -m "feat(audit): JSONL execution audit with param masking"
```

### Task 9: server 包——HTTP BFF(集成)

**Files:**
- Create: `internal/server/server.go`
- Test: `internal/server/server_test.go`(单元,stub Runner)
- Test: `internal/server/server_integration_test.go`(全链)

- [ ] **Step 1: 写失败单元测试(鉴权与错误形态)**

`internal/server/server_test.go`:

```go
//go:build !integration

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dayuer/plinth/internal/audit"
	"github.com/dayuer/plinth/internal/exec"
	"github.com/dayuer/plinth/internal/queryfile"
	"github.com/dayuer/plinth/internal/registry"
)

type stubRunner struct {
	gotQ *queryfile.Query
	gotA map[string]any
	err  error
}

func (s *stubRunner) Run(ctx context.Context, q *queryfile.Query, args map[string]any) (*exec.Result, error) {
	s.gotQ, s.gotA = q, args
	if s.err != nil {
		return nil, s.err
	}
	return &exec.Result{Rows: []map[string]any{{"id": int64(1)}}, DurationMs: 3}, nil
}

func newTestServer(t *testing.T, run Runner) (*Server, *httptest.Server) {
	t.Helper()
	reg := registry.NewForTest(
		&queryfile.Query{Name: "q1", Mode: "read", AllowTokens: []string{"web-bff"},
			Params: []queryfile.Param{{Name: "org", Type: "int", Required: true}},
			SQL:    "SELECT id FROM t WHERE org = :org"},
	)
	aud, err := audit.Open(t.TempDir()+"/a.jsonl", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = aud.Close() })
	s := &Server{Reg: reg, Run_: run, Tokens: map[string]string{"web-bff": "tok1", "worker": "tok2"}, Aud: aud}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return s, ts
}

func TestAuthMatrix(t *testing.T) {
	_, ts := newTestServer(t, &stubRunner{})
	post := func(token, body string) *http.Response {
		req, _ := http.NewRequest("POST", ts.URL+"/q/q1", strings.NewReader(body))
		req.Header.Set("X-Plinth-Token", token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	if r := post("", `{"org":1}`); r.StatusCode != 401 {
		t.Errorf("no token = %d", r.StatusCode)
	}
	if r := post("wrong", `{"org":1}`); r.StatusCode != 401 {
		t.Errorf("bad token = %d", r.StatusCode)
	}
	if r := post("tok2", `{"org":1}`); r.StatusCode != 403 {
		t.Errorf("token not allowed = %d", r.StatusCode)
	}
	if r := post("tok1", `{"org":1}`); r.StatusCode != 200 {
		t.Errorf("ok = %d", r.StatusCode)
	}
	if r := post("tok1", `{"org":"x"}`); r.StatusCode != 400 {
		t.Errorf("type error = %d", r.StatusCode)
	}
	req, _ := http.NewRequest("POST", ts.URL+"/q/ghost", strings.NewReader(`{}`))
	req.Header.Set("X-Plinth-Token", "tok1")
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != 404 {
		t.Errorf("missing query = %d", resp.StatusCode)
	}
}

func TestSuccessShape(t *testing.T) {
	stub := &stubRunner{}
	_, ts := newTestServer(t, stub)
	req, _ := http.NewRequest("POST", ts.URL+"/q/q1", strings.NewReader(`{"org":1}`))
	req.Header.Set("X-Plinth-Token", "tok1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Header.Get("X-Plinth-Rows") != "1" || resp.Header.Get("X-Plinth-Duration-Ms") != "3" {
		t.Errorf("headers = %v", resp.Header)
	}
	var body struct {
		Rows []map[string]any `json:"rows"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Rows) != 1 || body.Rows[0]["id"] != float64(1) {
		t.Errorf("body = %+v", body)
	}
	if stub.gotQ == nil || stub.gotQ.Name != "q1" {
		t.Error("runner did not receive query")
	}
}
```

(registry 需提供测试构造 `NewForTest(qs ...*queryfile.Query) *Registry`,Task 6 的实现里没有——本任务在 `internal/registry/registry.go` 追加:)

```go
// NewForTest builds a registry from explicit queries (test helper).
func NewForTest(qs ...*queryfile.Query) *Registry {
	r := &Registry{queries: map[string]*queryfile.Query{}}
	for _, q := range qs {
		r.queries[q.Name] = q
	}
	return r
}
```

- [ ] **Step 2: 写失败集成测试(全链含审计)**

`internal/server/server_integration_test.go`:

```go
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
	integration.ApplyFixture(t)
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
	s := &Server{
		Reg:    registry.NewForTest(q),
		Run_:   &exec.Engine{Pool: pool, DefaultTimeout: 5 * time.Second, MaxRows: 100},
		Tokens: map[string]string{"web-bff": "tok1"},
		Aud:    aud,
	}
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

	f, _ := os.Open(audPath)
	defer f.Close()
	sc := bufio.NewScanner(f)
	var recs []map[string]any
	for sc.Scan() {
		var m map[string]any
		json.Unmarshal(sc.Bytes(), &m)
		recs = append(recs, m)
	}
	if len(recs) != 1 || recs[0]["caller"] != "web-bff" || recs[0]["query"] != "invoice-list" {
		t.Fatalf("audit = %+v", recs)
	}
	if recs[0]["params"].(map[string]any)["status"] != "***" {
		t.Errorf("mask not applied: %v", recs[0]["params"])
	}
}
```

注意 `ApplyFixture` 签名调整为 `ApplyFixture(t)` 内部自建 pool(在 Task 7 的 `test/integration/pg.go` 中提供两个函数:`StartPG(t)` 与 `ApplyFixture(t)`——后者内部调 `StartPG`,供不带 pool 的测试用;带 pool 场景用 `ApplyFixtureOn(t, pool)`)。以 Task 7 落地为准:`ApplyFixtureOn(t, pool)` 执行 SQL,`ApplyFixture(t)` = `ApplyFixtureOn(t, StartPG(t))`。本测试用 `ApplyFixture(t)` 且 engine 复用其返回的 pool:`pool := integration.ApplyFixture(t)`。

- [ ] **Step 3: 跑测试确认失败**

Run: `go test ./internal/server/ -v && go test -tags integration ./internal/server/ -v`
Expected: FAIL(`undefined: Server`)

- [ ] **Step 4: 写实现**

`internal/server/server.go`:

```go
// Package server exposes the HTTP BFF: POST /q/{name} with a static
// service token (spec v2 §4).
package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/dayuer/plinth/internal/audit"
	"github.com/dayuer/plinth/internal/exec"
	"github.com/dayuer/plinth/internal/queryfile"
	"github.com/dayuer/plinth/internal/registry"
)

// Runner decouples the server from exec.Engine for testing.
type Runner interface {
	Run(ctx context.Context, q *queryfile.Query, args map[string]any) (*exec.Result, error)
}

type Server struct {
	Reg    *registry.Registry
	Run_   Runner
	Tokens map[string]string // caller -> token
	Aud    *audit.Writer
	Log    *slog.Logger
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /q/{name}", s.handleQuery)
	return mux
}

func (s *Server) handleQuery(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	q := s.Reg.Get(name)
	if q == nil {
		problem(w, http.StatusNotFound, "query not found", name)
		return
	}
	caller := s.callerFor(r.Header.Get("X-Plinth-Token"))
	if caller == "" {
		problem(w, http.StatusUnauthorized, "invalid token", "")
		return
	}
	if !q.Allows(caller) {
		problem(w, http.StatusForbidden, "token not allowed for this query", caller)
		return
	}
	var in map[string]any
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		problem(w, http.StatusBadRequest, "invalid JSON body", err.Error())
		return
	}
	if in == nil {
		in = map[string]any{}
	}
	res, err := s.Run_.Run(r.Context(), q, in)
	rec := audit.Record{TS: time.Now().UTC(), Caller: caller, Query: name, Params: in, Status: "ok"}
	if err != nil {
		rec.Status, rec.Err = "error", err.Error()
		if audErr := s.Aud.Record(rec); audErr != nil && s.Log != nil {
			s.Log.Error("audit write failed", "err", audErr)
		}
		problem(w, http.StatusInternalServerError, "execution failed", err.Error())
		return
	}
	rec.Rows, rec.Ms = len(res.Rows), res.DurationMs
	if audErr := s.Aud.Record(rec); audErr != nil && s.Log != nil {
		s.Log.Error("audit write failed", "err", audErr)
	}
	body, err := json.Marshal(map[string]any{"rows": res.Rows})
	if err != nil {
		problem(w, http.StatusInternalServerError, "encode failed", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Plinth-Rows", strconv.Itoa(len(res.Rows)))
	w.Header().Set("X-Plinth-Duration-Ms", strconv.FormatInt(res.DurationMs, 10))
	w.WriteHeader(http.StatusOK)
	w.Write(body)
}

func (s *Server) callerFor(token string) string {
	for caller, t := range s.Tokens {
		if t != "" && t == token {
			return caller
		}
	}
	return ""
}

func problem(w http.ResponseWriter, status int, title, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type": "about:blank", "title": title, "status": status, "detail": detail,
	})
}
```

(补 `"context"` import。)

- [ ] **Step 5: 跑测试确认通过**

Run: `go test ./internal/server/ -v && go test -tags integration ./internal/server/ -v`
Expected: 单元 + 集成 PASS

- [ ] **Step 6: Commit**

```bash
git add internal/server internal/registry test
git commit -m "feat(server): POST /q/{name} with static tokens, problem-details, audit hook"
```

### Task 10: CLI 全接线 + semantics pull + 文档 + v0.1.0

**Files:**
- Modify: `internal/cli/run.go`
- Create: `internal/cli/commands.go`
- Test: `internal/cli/commands_integration_test.go`
- Modify: `README.md`、`CHANGELOG.md`(新建)

- [ ] **Step 1: 写失败集成测试(CLI validate/serve 全链冒烟)**

`internal/cli/commands_integration_test.go`:

```go
//go:build integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dayuer/plinth/test/integration"
)

func writeProject(t *testing.T, dbURL string) string {
	t.Helper()
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "queries"), 0o755)
	os.WriteFile(filepath.Join(dir, "plinth.yml"), []byte("database:\n  url: "+dbURL+
		"\nauth:\n  tokens:\n    web-bff: tok1\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "queries", "invoice-list.sql"), []byte(
		`-- plinth: name: invoice-list
-- params: org_id:int:required | status:str:optional
-- allow-tokens: web-bff
SELECT id, status FROM invoices
WHERE org_id = :org_id AND (:status::text IS NULL OR status = :status)
ORDER BY id
`), 0o644)
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

	// bad query file → exit code 2
	os.WriteFile(filepath.Join(dir, "queries", "bad.sql"), []byte(
		"-- plinth: name: bad\n-- allow-tokens: web-bff\nDELETE FROM invoices\n"), 0o644)
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
	os.WriteFile(script, []byte("#!/bin/sh\necho 'datasets: invoices v1'\n"), 0o755)
	os.WriteFile(filepath.Join(dir, "plinth.yml"), []byte("database:\n  url: "+pool.Config().ConnString()+
		"\nauth:\n  tokens: {}\nsemantics:\n  pull_command: "+script+"\n"), 0o644)

	if err := Run([]string{"pull", "--dir", dir}); err != nil {
		t.Fatalf("pull: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "semantics", "datasets.yml")); err != nil {
		t.Fatal("semantics snapshot not written")
	}
	// validate passes; drift prints warning but exit 0 (query snapshot missing)
	if err := Run([]string{"validate", "--dir", dir}); err != nil {
		t.Fatalf("validate with semantics: %v", err)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test -tags integration ./internal/cli/ -v`
Expected: FAIL(not implemented)

- [ ] **Step 3: 写实现**

`internal/cli/commands.go`:

```go
package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/dayuer/plinth/internal/audit"
	"github.com/dayuer/plinth/internal/exec"
	"github.com/dayuer/plinth/internal/meta"
	"github.com/dayuer/plinth/internal/registry"
	"github.com/dayuer/plinth/internal/server"
	"github.com/jackc/pgx/v5/pgxpool"
)

func metaDir(args []string, name string) (*meta.Config, string, *flag.FlagSet) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	dir := fs.String("dir", ".", "project directory")
	fs.Parse(args)
	cfg, err := meta.LoadConfig(filepath.Join(*dir, "plinth.yml"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	return cfg, *dir, fs
}

func runValidate(args []string) error {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	dir := fs.String("dir", ".", "project directory")
	fs.Parse(args)
	reg, errs := registry.LoadDir(*dir)
	warnSemanticsDrift(*dir, reg)
	if len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintln(os.Stderr, "validate:", e)
		}
		return &MetaError{Err: fmt.Errorf("%d query problems", len(errs))}
	}
	fmt.Printf("validate: ok (%d queries)\n", len(reg.Names()))
	return nil
}

// warnSemanticsDrift prints a warning when a query pins a snapshot older
// than the current semantics snapshot (spec v2 §7).
func warnSemanticsDrift(dir string, reg *registry.Registry) {
	cur, err := os.ReadFile(filepath.Join(dir, "semantics", "snapshot.txt"))
	if err != nil {
		return
	}
	current := strings.TrimSpace(string(cur))
	for _, name := range reg.Names() {
		q := reg.Get(name)
		if q.SemDataset != "" && q.SemSnapshot != current {
			fmt.Fprintf(os.Stderr, "warning: query %s pins snapshot %s, current is %s — review SQL against new semantics\n",
				name, q.SemSnapshot, current)
		}
	}
}

func runTest(args []string) error {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	dir := fs.String("dir", ".", "project directory")
	name := fs.String("query", "", "query name to test")
	params := fs.String("param", "", "k=v pairs, comma separated (values parsed by declared type)")
	fs.Parse(args)
	if *name == "" {
		return fmt.Errorf("test: --query required")
	}
	cfg, d, _ := metaDir(nil, "test")
	_ = cfg
	reg, errs := registry.LoadDir(d)
	if len(errs) > 0 {
		return &MetaError{Err: errs[0]}
	}
	q := reg.Get(*name)
	if q == nil {
		return &MetaError{Err: fmt.Errorf("query %q not found", *name)}
	}
	in := map[string]any{}
	if *params != "" {
		for _, kv := range strings.Split(*params, ",") {
			parts := strings.SplitN(kv, "=", 2)
			if len(parts) != 2 {
				return &MetaError{Err: fmt.Errorf("bad --param %q", kv)}
			}
			in[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	// string values need typing: re-type via declared param types
	typed := map[string]any{}
	for k, v := range in {
		for _, p := range q.Params {
			if p.Name == k {
				typed[k] = retype(p.Type, fmt.Sprintf("%v", v))
			}
		}
	}
	cfg2, err := meta.LoadConfig(filepath.Join(d, "plinth.yml"))
	if err != nil {
		return &MetaError{Err: err}
	}
	pool, err := pgxpool.New(context.Background(), cfg2.Database.URL)
	if err != nil {
		return &DBError{Err: err}
	}
	defer pool.Close()
	eng := &exec.Engine{Pool: pool, DefaultTimeout: time.Duration(cfg2.Engine.DefaultTimeoutMs) * time.Millisecond, MaxRows: 5}
	res, err := eng.Run(context.Background(), q, typed)
	if err != nil {
		return &DBError{Err: err}
	}
	fmt.Printf("test %s: %d rows (capped at 5)\n", *name, len(res.Rows))
	for i, row := range res.Rows {
		fmt.Printf("  row[%d] = %v\n", i, row)
	}
	return nil
}

func retype(typ, s string) any {
	switch typ {
	case "int":
		var n int64
		fmt.Sscanf(s, "%d", &n)
		return float64(n) // coerce accepts integral float64
	case "float":
		var f float64
		fmt.Sscanf(s, "%g", &f)
		return f
	case "bool":
		return s == "true" || s == "1"
	}
	return s
}

func runPull(args []string) error {
	cfg, dir, _ := metaDir(args, "pull")
	fields := strings.Fields(cfg.Semantics.PullCommand)
	if len(fields) == 0 {
		return &MetaError{Err: fmt.Errorf("semantics.pull_command not configured")}
	}
	cmd := exec.Command(fields[0], fields[1:]...)
	out, err := cmd.Output()
	if err != nil {
		return &DBError{Err: fmt.Errorf("pull_command failed: %w", err)}
	}
	semDir := filepath.Join(dir, "semantics")
	if err := os.MkdirAll(semDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(semDir, "datasets.yml"), out, 0o644); err != nil {
		return err
	}
	sum := sha256.Sum256(out)
	version := hex.EncodeToString(sum[:])[:12]
	if err := os.WriteFile(filepath.Join(semDir, "snapshot.txt"), []byte(version+"\n"), 0o644); err != nil {
		return err
	}
	fmt.Printf("semantics: pulled %d bytes, snapshot version %s\n", len(out), version)
	fmt.Println("write this version into your queries' 'semantics:' header as you review them")
	return nil
}

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	dir := fs.String("dir", ".", "project directory")
	addr := fs.String("addr", ":8080", "listen address")
	fs.Parse(args)
	cfg, err := meta.LoadConfig(filepath.Join(*dir, "plinth.yml"))
	if err != nil {
		return &MetaError{Err: err}
	}
	reg, errs := registry.LoadDir(*dir)
	if len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintln(os.Stderr, e)
		}
		return &MetaError{Err: fmt.Errorf("refusing to serve with %d query problems", len(errs))}
	}
	pool, err := pgxpool.New(context.Background(), cfg.Database.URL)
	if err != nil {
		return &DBError{Err: err}
	}
	defer pool.Close()
	aud, err := audit.Open(cfg.Audit.Path, cfg.Audit.MaskParams)
	if err != nil {
		return &DBError{Err: err}
	}
	defer aud.Close()
	srv := &server.Server{
		Reg:    reg,
		Run_:   &exec.Engine{Pool: pool, DefaultTimeout: time.Duration(cfg.Engine.DefaultTimeoutMs) * time.Millisecond, MaxRows: cfg.Engine.MaxRows},
		Tokens: cfg.Auth.Tokens,
		Aud:    aud,
	}
	httpSrv := &http.Server{Addr: *addr, Handler: srv.Handler()}

	// SIGHUP reloads the query registry (spec v2 §4).
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGHUP)
		for range sig {
			newReg, errs := registry.LoadDir(*dir)
			if len(errs) > 0 {
				fmt.Fprintf(os.Stderr, "reload aborted: %d problems\n", len(errs))
				continue
			}
			srv.Reg = newReg
			fmt.Println("reload: ok")
		}
	}()

	fmt.Printf("plinth serving %d queries on %s\n", len(reg.Names()), *addr)
	if err := httpSrv.ListenAndServe(); err != nil {
		return &DBError{Err: err}
	}
	return nil
}

func runStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	dir := fs.String("dir", ".", "project directory")
	fs.Parse(args)
	reg, _ := registry.LoadDir(*dir)
	fmt.Printf("queries: %d (%s)\n", len(reg.Names()), strings.Join(reg.Names(), ", "))
	cfg, err := meta.LoadConfig(filepath.Join(*dir, "plinth.yml"))
	if err == nil && cfg.Audit.Path != "" {
		if b, err := os.ReadFile(cfg.Audit.Path); err == nil {
			lines := strings.Split(strings.TrimSpace(string(b)), "\n")
			n := len(lines)
			start := n - 5
			if start < 0 {
				start = 0
			}
			for _, ln := range lines[start:] {
				if ln != "" {
					fmt.Println(ln)
				}
			}
		}
	}
	return nil
}
```

(需要补 `"net/http"` import;`runTest` 里对 `metaDir` 的首次调用删除,统一走 `meta.LoadConfig` 一次。)

`internal/cli/run.go` 的 switch 替换为:

```go
	switch args[0] {
	case "validate":
		return runValidate(args[1:])
	case "test":
		return runTest(args[1:])
	case "pull":
		return runPull(args[1:])
	case "serve":
		return runServe(args[1:])
	case "status":
		return runStatus(args[1:])
	default:
		usage()
		return fmt.Errorf("unknown command %q", args[0])
	}
```

- [ ] **Step 4: 跑全部测试确认通过**

Run: `go test ./... -v && go test -tags integration ./... -count=1 -v`
Expected: 全绿(无 Docker SKIP)

- [ ] **Step 5: 文档与发版**

README 快速上手节、CHANGELOG v0.1.0(条目:validate/test/pull/serve/status 五命令、只读双保险、命名参数、JSONL 审计)、`git tag v0.1.0`。

```bash
git add -A
git commit -m "feat(cli): validate/test/pull/serve/status wired end-to-end; docs; v0.1.0"
git push origin feat/plan-1-v2 --tags
```

---

## Self-Review 记录

- **Spec 覆盖**:§3 文件格式(Task 3)、§4 流水线与热加载(Task 9、10)、§5 双保险(Task 5 静态闸 + 连接串只读角色由部署方保证,fixture 内建只读角色演示)、§6 审计(Task 8、9)、§7 语义同步(Task 10)、§9 测试策略(各任务 + 语料)。§8 agent 循环 = CLI 组合(Task 10),MCP 属后续版本。
- **占位符**:commands.go 中标注的两处整理指令(import 合并、metaDir 冗余调用删除)已写明改法,无 TBD。
- **类型一致性**:`queryfile.Query` 字段贯穿 Task 7/9/10;`registry.NewForTest` 在 Task 9 步骤内补充定义;`ApplyFixtureOn/ApplyFixture` 双签名在 Task 7/9 步骤内写明。
