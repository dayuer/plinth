# Plinth Plan 1: 地基(脚手架/metadata/表达式语言/内省/catalog/CLI)Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 交付 `plinth validate` 与 `plinth diff` 两个 CLI 子命令:加载 YAML metadata、内省真实 PostgreSQL、交叉校验(含行过滤表达式解析与参数化编译、注入语料与 fuzz),元数据错误退出码 2、库漂移退出码 3。

**Architecture:** 单 Go module 多 package:`meta`(YAML 类型与加载)、`policy`(表达式 tokenizer→parser→compiler,纯函数无 IO)、`introspect`(information_schema/pg_catalog 读取)、`catalog`(内省×注解×策略交叉校验)、`cli`(子命令)。本计划不启动任何 HTTP/事件/MCP,产出是独立可用的静态工具。

**Tech Stack:** Go 1.24+、`github.com/jackc/pgx/v5`、`gopkg.in/yaml.v3`、`github.com/testcontainers/testcontainers-go`(+postgres module,集成测试)、GitHub Actions。

**Spec:** `docs/specs/2026-08-16-plinth-design.md`(§3 metadata、§4 表达式与安全、§7 退出码)。一处小修正:policies YAML 增加 `schema:` 字段(默认 `public`),与 models 对齐,写回 spec 属 Plan 1 Task 11 的文档同步项。

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
# 确认 go.mod 的 go 指令行 >= 1.24(测试用 t.Context(),1.24 起提供)
mkdir -p cmd/plinth internal/cli .github/workflows
```

- [ ] **Step 2: 写 main.go 与 cli 骨架**

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

// ExitCode maps errors to process exit codes (spec §7):
// 2 = metadata invalid, 3 = database unreachable or drift.
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
	fmt.Println(`plinth - data foundation gateway for existing PostgreSQL

Usage:
  plinth validate [--dir DIR]   load and validate metadata (no database needed)
  plinth diff     [--dir DIR]   validate against the live database (drift check)
  plinth serve                  (Plan 2) start the gateway`)
}

// Run dispatches subcommands.
func Run(args []string) error {
	if len(args) == 0 {
		usage()
		return nil
	}
	switch args[0] {
	case "validate", "diff", "serve":
		return &MetaError{Err: fmt.Errorf("%s: not implemented yet (Plan 1 in progress)", args[0])}
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
```

- [ ] **Step 4: 验证构建与 usage**

Run: `go build ./... && go run ./cmd/plinth`
Expected: 打印 usage,退出码 0

Run: `go run ./cmd/plinth validate; echo "exit=$?"`
Expected: stderr `plinth: validate: not implemented yet (Plan 1 in progress)`,输出 `exit=2`

- [ ] **Step 5: Commit**

```bash
git add go.mod cmd internal .github Makefile .gitignore
git commit -m "feat: module scaffold, CLI dispatch with exit-code mapping, CI"
```

### Task 2: meta 包——类型、加载与环境变量展开

**Files:**
- Create: `internal/meta/types.go`
- Create: `internal/meta/load.go`
- Test: `internal/meta/load_test.go`

- [ ] **Step 1: 写失败测试**

`internal/meta/load_test.go`:

```go
package meta

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const cfgYAML = `database:
  url: ${TEST_DATABASE_URL}
auth:
  jwks_url: https://api.example.com/.well-known/jwks.json
  roles_claim: role
  claims: [sub, org, role]
storage:
  path: /var/lib/plinth/state.db
events:
  mode: logical
  slot_name: plinth_slot
`

const modelYAML = `schema: public
table: invoices
expose: true
columns:
  hide: [internal_note]
relations:
  - name: buyer
    type: many-to-one
    on: buyer_id
    references: { table: buyers, column: id }
    expose: true
`

const policyYAML = `schema: public
table: invoices
rules:
  - role: accountant
    columns: { allow: "*", deny: [internal_note] }
    row: org_id == $token.org and status != 'VOID'
  - role: ops
    columns:
      allow: [id, status, buyer_id]
    row: true
  - role: "*"
    columns: { allow: [] }
    row: false
`

func TestLoadDir(t *testing.T) {
	t.Setenv("TEST_DATABASE_URL", "postgres://u:p@localhost:5432/db")
	dir := writeTree(t, map[string]string{
		"plinth.yml":             cfgYAML,
		"models/invoices.yml":    modelYAML,
		"policies/invoices.yml":  policyYAML,
		"models/README.md":       "not yaml, ignored",
	})

	cfg, models, policies, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.URL != "postgres://u:p@localhost:5432/db" {
		t.Errorf("env not expanded: %q", cfg.Database.URL)
	}
	if len(models) != 1 || models[0].Table != "invoices" || !models[0].Expose {
		t.Fatalf("models = %+v", models)
	}
	if got := models[0].Columns.Hide; len(got) != 1 || got[0] != "internal_note" {
		t.Errorf("hide = %v", got)
	}
	if len(models[0].Relations) != 1 || models[0].Relations[0].On != "buyer_id" {
		t.Errorf("relations = %+v", models[0].Relations)
	}
	if len(policies) != 1 || len(policies[0].Rules) != 3 {
		t.Fatalf("policies = %+v", policies)
	}
	if policies[0].Rules[0].Row != "org_id == $token.org and status != 'VOID'" {
		t.Errorf("row = %q", policies[0].Rules[0].Row)
	}
}

func TestLoadDirMissingConfig(t *testing.T) {
	dir := writeTree(t, map[string]string{"models/a.yml": modelYAML})
	if _, _, _, err := LoadDir(dir); err == nil {
		t.Fatal("expected error for missing plinth.yml")
	}
}

func TestLoadDirBadYAML(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"plinth.yml":          cfgYAML,
		"models/broken.yml":   "table: [unclosed",
	})
	if _, _, _, err := LoadDir(dir); err == nil {
		t.Fatal("expected error for broken yaml")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/meta/ -run TestLoadDir -v`
Expected: FAIL(`undefined: LoadDir`)

- [ ] **Step 3: 写实现**

`internal/meta/types.go`:

```go
// Package meta defines Plinth's on-disk YAML metadata:
// plinth.yml (config), models/*.yml, policies/*.yml.
package meta

// Config is plinth.yml.
type Config struct {
	Database struct {
		URL string `yaml:"url"`
	} `yaml:"database"`
	Auth struct {
		JWKSURL    string   `yaml:"jwks_url"`
		RolesClaim string   `yaml:"roles_claim"`
		Claims     []string `yaml:"claims"` // allowlist of $token.<name> claims
	} `yaml:"auth"`
	Storage struct {
		Path string `yaml:"path"`
	} `yaml:"storage"`
	Events struct {
		Mode     string `yaml:"mode"` // logical | polling
		SlotName string `yaml:"slot_name"`
	} `yaml:"events"`
}

// Model is one file under models/.
type Model struct {
	Schema    string     `yaml:"schema"`
	Table     string     `yaml:"table"`
	Expose    bool       `yaml:"expose"`
	Columns   Columns    `yaml:"columns"`
	Relations []Relation `yaml:"relations"`
}

type Columns struct {
	Hide []string `yaml:"hide"`
}

type Relation struct {
	Name       string `yaml:"name"`
	Type       string `yaml:"type"` // many-to-one (only MVP type)
	On         string `yaml:"on"`
	References Ref    `yaml:"references"`
	Expose     bool   `yaml:"expose"`
}

type Ref struct {
	Table  string `yaml:"table"`
	Column string `yaml:"column"`
}

// Policy is one file under policies/.
type Policy struct {
	Schema string `yaml:"schema"`
	Table  string `yaml:"table"`
	Rules  []Rule `yaml:"rules"`
}

type Rule struct {
	Role    string      `yaml:"role"`
	Columns ColumnRules `yaml:"columns"`
	Row     string      `yaml:"row"` // expression source text
}

type ColumnRules struct {
	Allow []string `yaml:"allow"` // "*" allowed, only alone
	Deny  []string `yaml:"deny"`
}
```

`internal/meta/load.go`:

```go
package meta

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// LoadDir reads DIR/plinth.yml, DIR/models/*.yml, DIR/policies/*.yml.
// ${VAR} in database.url is expanded from the environment.
func LoadDir(dir string) (*Config, []Model, []Policy, error) {
	var cfg Config
	b, err := os.ReadFile(filepath.Join(dir, "plinth.yml"))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("plinth.yml: %w", err)
	}
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, nil, nil, fmt.Errorf("plinth.yml: %w", err)
	}
	cfg.Database.URL = os.ExpandEnv(cfg.Database.URL)

	models, err := loadDirFiles[Model](filepath.Join(dir, "models"))
	if err != nil {
		return nil, nil, nil, err
	}
	policies, err := loadDirFiles[Policy](filepath.Join(dir, "policies"))
	if err != nil {
		return nil, nil, nil, err
	}
	return &cfg, models, policies, nil
}

func loadDirFiles[T any](dir string) ([]T, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil // empty dir is fine
	}
	if err != nil {
		return nil, err
	}
	var out []T
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yml") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		var v T
		if err := yaml.Unmarshal(b, &v); err != nil {
			return nil, fmt.Errorf("%s/%s: %w", dir, e.Name(), err)
		}
		out = append(out, v)
	}
	return out, nil
}
```

- [ ] **Step 4: 拉依赖并跑测试确认通过**

Run: `go get gopkg.in/yaml.v3 && go test ./internal/meta/ -v`
Expected: 3 个测试全 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/meta go.mod go.sum
git commit -m "feat(meta): YAML types and loader with env expansion"
```

### Task 3: policy 包——表达式 tokenizer 与 parser

**Files:**
- Create: `internal/policy/ast.go`
- Create: `internal/policy/lexer.go`
- Create: `internal/policy/parser.go`
- Test: `internal/policy/parser_test.go`

表达式语法(spec §4):`or < and < not < 比较/成员判定`;比较符 `== != > >= < <=`;`in (字面量列表)`;`is [not] null`;操作数为列名(`[a-z_][a-z0-9_]*`)、声明(`$token.<name>`)或字面量(`'str'`、整数、`true/false`);裸列 = 布尔真值;`null` 只能出现在 `is` 后。

- [ ] **Step 1: 写失败测试**

`internal/policy/parser_test.go`:

```go
package policy

import "testing"

func TestParseValid(t *testing.T) {
	cases := []string{
		"true",
		"active",
		"org_id == $token.org",
		"org_id == $token.org and status != 'VOID'",
		"amount_total >= 100 and currency in ('IDR', 'USD')",
		"not deleted_at is null and (a == 1 or b == 2)",
		"role in ('admin', 'ops') and org_id == $token.org",
	}
	for _, src := range cases {
		if _, err := Parse(src); err != nil {
			t.Errorf("Parse(%q) error: %v", src, err)
		}
	}
}

func TestParseShapes(t *testing.T) {
	e, err := Parse("org_id == $token.org and status != 'VOID'")
	if err != nil {
		t.Fatal(err)
	}
	b, ok := e.(Binary)
	if !ok || b.Op != "and" {
		t.Fatalf("root = %#v", e)
	}
	l, ok := b.L.(Binary)
	if !ok || l.Op != "==" {
		t.Fatalf("left = %#v", b.L)
	}
	if c, ok := l.L.(Column); !ok || c.Name != "org_id" {
		t.Fatalf("left.left = %#v", l.L)
	}
	if c, ok := l.R.(Claim); !ok || c.Name != "org" {
		t.Fatalf("left.right = %#v", l.R)
	}
	r, ok := b.R.(Binary)
	if !ok || r.Op != "!=" {
		t.Fatalf("right = %#v", b.R)
	}
	if lit, ok := r.R.(Literal); !ok || lit.Kind != "str" || lit.Val != "VOID" {
		t.Fatalf("right.right = %#v", r.R)
	}
}

func TestParsePrecedence(t *testing.T) {
	// "a or b and c" must group as a or (b and c)
	e, err := Parse("a or b and c")
	if err != nil {
		t.Fatal(err)
	}
	root := e.(Binary)
	if root.Op != "or" {
		t.Fatalf("root op = %s", root.Op)
	}
	if _, ok := root.R.(Binary); !ok {
		t.Fatal("right side should be a nested Binary")
	}
}

func TestParseErrors(t *testing.T) {
	cases := []string{
		"",                       // empty
		"org_id ==",              // dangling
		"foo;",                   // trailing junk
		"org_id == 'unclosed",    // unterminated string
		"$token.",                // empty claim
		"1 == 1",                 // literal compared to literal: left must be column/claim
		"$token.org == $token.sub", // claim compared to claim: invalid
		"org_id in (org_id)",     // in-list accepts literals only
		"org_id is 'x'",          // is only accepts null
		"org_id == null",         // null only via is
		"Bad_Column == 1",        // uppercase ident rejected
		"x in ()",                // empty list
		"a == 1 and",             // trailing and
	}
	for _, src := range cases {
		if _, err := Parse(src); err == nil {
			t.Errorf("Parse(%q): expected error, got nil", src)
		}
	}
}
```

注:`1 == 1` 与 `$token.org == $token.sub` 这两条在 parser 层拒绝:比较的左侧必须是列名或声明,右侧必须是列名、声明或字面量,且**左右不能同时为声明**(没有业务含义且掩盖笔误)。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/policy/ -v`
Expected: FAIL(`undefined: Parse`)

- [ ] **Step 3: 写 AST、lexer、parser**

`internal/policy/ast.go`:

```go
// Package policy implements the row-filter expression language:
// parse to AST, compile to a parameterized SQL fragment. No I/O.
package policy

// Expr is a node of the expression AST.
type Expr interface{ expr() }

type Binary struct {         // and/or or comparison ops
	Op   string              // "and","or","==","!=",">",">=","<","<="
	L, R Expr
}

type Not struct{ X Expr }

type In struct {             // col in (literal, ...)
	Col  Column
	Vals []Literal
}

type IsNull struct {         // col is [not] null
	Col Column
	Not bool
}

type Column struct{ Name string }

type Claim struct{ Name string } // $token.<Name>

type Literal struct {
	Kind string // "str" | "int" | "bool"
	Val  any
}

func (Binary) expr() {}
func (Not) expr()    {}
func (In) expr()     {}
func (IsNull) expr() {}
func (Column) expr() {}
func (Claim) expr()  {}
func (Literal) expr() {}

// ColumnsUsed returns every column name referenced in the expression.
func ColumnsUsed(e Expr) []string {
	var out []string
	var walk func(Expr)
	walk = func(x Expr) {
		switch n := x.(type) {
		case Binary:
			walk(n.L)
			walk(n.R)
		case Not:
			walk(n.X)
		case In:
			out = append(out, n.Col.Name)
		case IsNull:
			out = append(out, n.Col.Name)
		case Column:
			out = append(out, n.Name)
		}
	}
	walk(e)
	return out
}

// ClaimsUsed returns every claim name referenced in the expression.
func ClaimsUsed(e Expr) []string {
	var out []string
	var walk func(Expr)
	walk = func(x Expr) {
		switch n := x.(type) {
		case Binary:
			walk(n.L)
			walk(n.R)
		case Not:
			walk(n.X)
		case Claim:
			out = append(out, n.Name)
		}
	}
	walk(e)
	return out
}
```

`internal/policy/lexer.go`:

```go
package policy

import (
	"fmt"
	"strings"
)

type tokKind int

const (
	tEOF tokKind = iota
	tIdent
	tClaim
	tString
	tNumber
	tOp
	tLParen
	tRParen
	tComma
	tKw // and or not in is true false null
)

type token struct {
	kind tokKind
	text string
}

func isIdentStart(c byte) bool { return c == '_' || (c >= 'a' && c <= 'z') }

func isIdentChar(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')
}

func tokenize(src string) ([]token, error) {
	var toks []token
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '(':
			toks = append(toks, token{tLParen, "("})
			i++
		case c == ')':
			toks = append(toks, token{tRParen, ")"})
			i++
		case c == ',':
			toks = append(toks, token{tComma, ","})
			i++
		case c == '\'':
			j := i + 1
			for j < len(src) && src[j] != '\'' {
				j++
			}
			if j >= len(src) {
				return nil, fmt.Errorf("unterminated string at offset %d", i)
			}
			toks = append(toks, token{tString, src[i+1 : j]})
			i = j + 1
		case c == '$':
			const p = "$token."
			if !strings.HasPrefix(src[i:], p) {
				return nil, fmt.Errorf("'$' is only valid as $token.<claim> at offset %d", i)
			}
			j := i + len(p)
			k := j
			if k < len(src) && !isIdentStart(src[k]) {
				return nil, fmt.Errorf("empty claim name at offset %d", i)
			}
			for k < len(src) && isIdentChar(src[k]) {
				k++
			}
			toks = append(toks, token{tClaim, src[j:k]})
			i = k
		case c == '=' || c == '!' || c == '<' || c == '>':
			two := ""
			if i+1 < len(src) {
				two = src[i : i+2]
			}
			switch two {
			case "==", "!=", ">=", "<=":
				toks = append(toks, token{tOp, two})
				i += 2
			default:
				if c == '!' || c == '=' {
					return nil, fmt.Errorf("bad operator %q at offset %d", two, i)
				}
				toks = append(toks, token{tOp, string(c)})
				i++
			}
		case c >= '0' && c <= '9':
			j := i
			for j < len(src) && src[j] >= '0' && src[j] <= '9' {
				j++
			}
			toks = append(toks, token{tNumber, src[i:j]})
			i = j
		case isIdentStart(c):
			j := i + 1
			for j < len(src) && isIdentChar(src[j]) {
				j++
			}
			w := src[i:j]
			switch w {
			case "and", "or", "not", "in", "is", "true", "false", "null":
				toks = append(toks, token{tKw, w})
			default:
				toks = append(toks, token{tIdent, w})
			}
			i = j
		default:
			return nil, fmt.Errorf("unexpected character %q at offset %d", c, i)
		}
	}
	return append(toks, token{tEOF, ""}), nil
}
```

`internal/policy/parser.go`:

```go
package policy

import "fmt"

// Parse parses a row-filter expression into an AST.
func Parse(src string) (Expr, error) {
	toks, err := tokenize(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	e, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tEOF {
		return nil, fmt.Errorf("unexpected trailing input near %q", p.peek().text)
	}
	return e, nil
}

type parser struct {
	toks []token
	pos  int
}

func (p *parser) peek() token { return p.toks[p.pos] }

func (p *parser) next() token {
	t := p.toks[p.pos]
	p.pos++
	return t
}

func (p *parser) accept(k tokKind, text string) bool {
	t := p.peek()
	if t.kind == k && (text == "" || t.text == text) {
		p.pos++
		return true
	}
	return false
}

func (p *parser) parseOr() (Expr, error) {
	l, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.accept(tKw, "or") {
		r, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		l = Binary{Op: "or", L: l, R: r}
	}
	return l, nil
}

func (p *parser) parseAnd() (Expr, error) {
	l, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	for p.accept(tKw, "and") {
		r, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		l = Binary{Op: "and", L: l, R: r}
	}
	return l, nil
}

func (p *parser) parseNot() (Expr, error) {
	if p.accept(tKw, "not") {
		x, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		return Not{X: x}, nil
	}
	return p.parseCmp()
}

func (p *parser) parseCmp() (Expr, error) {
	l, err := p.parseOperand()
	if err != nil {
		return nil, err
	}
	switch t := p.peek(); {
	case t.kind == tKw && t.text == "in":
		p.next()
		return p.parseIn(l)
	case t.kind == tKw && t.text == "is":
		p.next()
		return p.parseIs(l)
	case t.kind == tOp:
		p.next()
		r, err := p.parseOperand()
		if err != nil {
			return nil, err
		}
		if _, isClaim := l.(Claim); isClaim {
			if _, rIsClaim := r.(Claim); rIsClaim {
				return nil, fmt.Errorf("claim-to-claim comparison is not allowed")
			}
		}
		if _, isLit := l.(Literal); isLit {
			return nil, fmt.Errorf("literal cannot be the left side of a comparison")
		}
		return Binary{Op: t.text, L: l, R: r}, nil
	}
	return l, nil // bare column / claim / literal in boolean context
}

func (p *parser) parseIn(l Expr) (Expr, error) {
	col, ok := l.(Column)
	if !ok {
		return nil, fmt.Errorf("left side of 'in' must be a column")
	}
	if !p.accept(tLParen, "") {
		return nil, fmt.Errorf("expected '(' after 'in'")
	}
	var vals []Literal
	for {
		t := p.peek()
		lit, err := literalOf(t)
		if err != nil {
			return nil, err
		}
		vals = append(vals, lit)
		p.next()
		if !p.accept(tComma, "") {
			break
		}
	}
	if len(vals) == 0 {
		return nil, fmt.Errorf("'in' list cannot be empty") // unreachable: loop requires >=1
	}
	if !p.accept(tRParen, "") {
		return nil, fmt.Errorf("expected ')' to close 'in' list")
	}
	return In{Col: col, Vals: vals}, nil
}

func (p *parser) parseIs(l Expr) (Expr, error) {
	col, ok := l.(Column)
	if !ok {
		return nil, fmt.Errorf("left side of 'is' must be a column")
	}
	neg := p.accept(tKw, "not")
	if !p.accept(tKw, "null") {
		return nil, fmt.Errorf("expected 'null' after 'is'")
	}
	return IsNull{Col: col, Not: neg}, nil
}

func (p *parser) parseOperand() (Expr, error) {
	t := p.next()
	switch t.kind {
	case tIdent:
		return Column{Name: t.text}, nil
	case tClaim:
		return Claim{Name: t.text}, nil
	case tString:
		return Literal{Kind: "str", Val: t.text}, nil
	case tNumber:
		n := 0
		for _, d := range t.text {
			n = n*10 + int(d-'0')
		}
		return Literal{Kind: "int", Val: n}, nil
	case tKw:
		if t.text == "true" {
			return Literal{Kind: "bool", Val: true}, nil
		}
		if t.text == "false" {
			return Literal{Kind: "bool", Val: false}, nil
		}
		return nil, fmt.Errorf("%q cannot be used as an operand", t.text)
	}
	return nil, fmt.Errorf("expected operand, got %q", t.text)
}

func literalOf(t token) (Literal, error) {
	switch t.kind {
	case tString:
		return Literal{Kind: "str", Val: t.text}, nil
	case tNumber:
		n := 0
		for _, d := range t.text {
			n = n*10 + int(d-'0')
		}
		return Literal{Kind: "int", Val: n}, nil
	case tKw:
		if t.text == "true" {
			return Literal{Kind: "bool", Val: true}, nil
		}
		if t.text == "false" {
			return Literal{Kind: "bool", Val: false}, nil
		}
	}
	return Literal{}, fmt.Errorf("'in' list accepts string/number/boolean literals only, got %q", t.text)
}
```

注:`"x in ()"` 用例实际由「循环至少要求一个字面量」挡住(`literalOf` 对 `tRParen` 报错),`len(vals)==0` 分支为防御性断言;`"a == 1 and"` 由 parseAnd 循环内 parseNot → parseOperand 撞 tEOF 报错。跑完测试确认这些路径真报错。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/policy/ -v`
Expected: TestParseValid / TestParseShapes / TestParsePrecedence / TestParseErrors 全 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/policy
git commit -m "feat(policy): row-filter expression lexer and recursive-descent parser"
```

### Task 4: policy 包——编译成参数化 SQL 与注入语料测试

**Files:**
- Create: `internal/policy/compile.go`
- Test: `internal/policy/compile_test.go`

- [ ] **Step 1: 写失败测试**

`internal/policy/compile_test.go`:

```go
package policy

import (
	"strings"
	"testing"
)

func compileSrc(t *testing.T, src string) *Compiled {
	t.Helper()
	e, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	c, err := Compile(e)
	if err != nil {
		t.Fatalf("Compile(%q): %v", src, err)
	}
	return c
}

func TestCompileBasic(t *testing.T) {
	c := compileSrc(t, "org_id == $token.org and status != 'VOID'")
	want := `("org_id" = $1 AND "status" != $2)`
	if c.SQL != want {
		t.Errorf("SQL = %s, want %s", c.SQL, want)
	}
	if len(c.Params) != 2 || c.Params[0].Claim != "org" || c.Params[1].Literal != "VOID" {
		t.Errorf("Params = %+v", c.Params)
	}
	if len(c.Claims) != 1 || c.Claims[0] != "org" {
		t.Errorf("Claims = %v", c.Claims)
	}
}

func TestCompileInAndNull(t *testing.T) {
	c := compileSrc(t, "currency in ('IDR', 'USD') and deleted_at is not null and active")
	if !strings.Contains(c.SQL, `"currency" IN ($1, $2)`) {
		t.Errorf("SQL = %s", c.SQL)
	}
	if !strings.Contains(c.SQL, `"deleted_at" IS NOT NULL`) {
		t.Errorf("SQL = %s", c.SQL)
	}
	if !strings.Contains(c.SQL, `AND "active"`) {
		t.Errorf("SQL = %s", c.SQL)
	}
}

func TestBind(t *testing.T) {
	c := compileSrc(t, "org_id == $token.org and role in ('admin', 'ops')")
	args, err := Bind(c, map[string]any{"org": int64(42)})
	if err != nil {
		t.Fatal(err)
	}
	if args[0] != int64(42) || args[1] != "admin" || args[2] != "ops" {
		t.Errorf("args = %v", args)
	}
	if _, err := Bind(c, nil); err == nil {
		t.Error("expected error for missing claim")
	}
}

// TestInjectionCorpus is a release gate (spec §8): hostile input must
// never leak into the SQL text — it can only fail parsing or land in a
// bound parameter.
func TestInjectionCorpus(t *testing.T) {
	hostile := []string{
		`' OR '1'='1`,
		`org_id == 1; DROP TABLE invoices`,
		`org_id == 1 -- comment`,
		`status = 'x'; SELECT pg_sleep(10)`,
		`org_id == 'x'' OR 1=1'`,
		"org_id == 'x\x00'",
		`org_id == '🦠'`,
		`"org_id" == 1`,
		`org_id == 'x' AND pg_sleep(10) IS NULL`,
		`$token.org'; --`,
		`org_id in ('a', 'b'); DELETE FROM x`,
		`Org_Id == 1`,
		`org_id == 'x' or '1'`,
	}
	for _, src := range hostile {
		e, perr := Parse(src)
		if perr != nil {
			continue // rejected at parse time: fine
		}
		c, cerr := Compile(e)
		if cerr != nil {
			continue // rejected at compile time: fine
		}
		for _, marker := []string{";", "--", "/*", "PG_SLEEP", "DROP", "DELETE"} {
			if strings.Contains(strings.ToUpper(c.SQL), marker) {
				t.Errorf("hostile src %q leaked marker %q into SQL %s", src, marker, c.SQL)
			}
		}
		// every string literal must be a bound param, not inlined
		for _, p := range c.Params {
			if p.Claim == "" {
				if s, ok := p.Literal.(string); ok && len(s) > 0 && strings.Contains(c.SQL, "'"+s+"'") {
					t.Errorf("hostile src %q inlined literal into SQL %s", src, c.SQL)
				}
			}
		}
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/policy/ -run TestCompile -v`
Expected: FAIL(`undefined: Compile`)

- [ ] **Step 3: 写实现**

`internal/policy/compile.go`:

```go
package policy

import (
	"fmt"
	"strings"
)

// Param is one bind parameter: either a fixed literal or a token claim
// resolved per request.
type Param struct {
	Literal any
	Claim   string
}

// Compiled is the result of compiling one expression.
type Compiled struct {
	SQL    string   // parameterized fragment, placeholders $1..$n
	Params []Param  // in placeholder order
	Claims []string // claim names used, deduplicated
}

// Compile turns an AST into a parameterized SQL fragment.
func Compile(e Expr) (*Compiled, error) {
	c := &compiler{}
	sql, err := c.expr(e)
	if err != nil {
		return nil, err
	}
	return &Compiled{SQL: sql, Params: c.params, Claims: c.claims}, nil
}

type compiler struct {
	params []Param
	claims []string
}

func (c *compiler) add(p Param) string {
	c.params = append(c.params, p)
	return fmt.Sprintf("$%d", len(c.params))
}

func (c *compiler) addClaim(name string) (string, error) {
	found := false
	for _, cl := range c.claims {
		if cl == name {
			found = true
			break
		}
	}
	if !found {
		c.claims = append(c.claims, name)
	}
	return c.add(Param{Claim: name}), nil
}

func (c *compiler) expr(e Expr) (string, error) {
	switch x := e.(type) {
	case Binary:
		if x.Op == "and" || x.Op == "or" {
			l, err := c.expr(x.L)
			if err != nil {
				return "", err
			}
			r, err := c.expr(x.R)
			if err != nil {
				return "", err
			}
			return "(" + l + " " + strings.ToUpper(x.Op) + " " + r + ")", nil
		}
		l, err := c.operand(x.L)
		if err != nil {
			return "", err
		}
		r, err := c.operand(x.R)
		if err != nil {
			return "", err
		}
		return l + " " + sqlOp(x.Op) + " " + r, nil
	case Not:
		inner, err := c.expr(x.X)
		if err != nil {
			return "", err
		}
		return "NOT (" + inner + ")", nil
	case In:
		ph := make([]string, len(x.Vals))
		for i, v := range x.Vals {
			ph[i] = c.add(Param{Literal: v.Val})
		}
		return quoteIdent(x.Col.Name) + " IN (" + strings.Join(ph, ", ") + ")", nil
	case IsNull:
		if x.Not {
			return quoteIdent(x.Col.Name) + " IS NOT NULL", nil
		}
		return quoteIdent(x.Col.Name) + " IS NULL", nil
	case Column:
		return quoteIdent(x.Name), nil
	case Claim:
		return "", fmt.Errorf("bare claim cannot be a boolean expression")
	case Literal:
		if x.Kind == "bool" {
			return strings.ToUpper(fmt.Sprintf("%v", x.Val)), nil
		}
		return "", fmt.Errorf("bare literal cannot be a boolean expression")
	}
	return "", fmt.Errorf("unknown node %T", e)
}

func (c *compiler) operand(e Expr) (string, error) {
	switch x := e.(type) {
	case Column:
		return quoteIdent(x.Name), nil
	case Claim:
		return c.addClaim(x.Name)
	case Literal:
		return c.add(Param{Literal: x.Val}), nil
	}
	return "", fmt.Errorf("invalid operand %T", e)
}

func sqlOp(op string) string {
	switch op {
	case "==":
		return "="
	case "!=":
		return "<>"
	default:
		return op
	}
}

// quoteIdent is safe against identifier injection because the lexer only
// accepts [a-z_][a-z0-9_]* for columns and claims.
func quoteIdent(name string) string {
	return `"` + name + `"`
}

// Bind resolves claim params against the request's token claims.
func Bind(c *Compiled, claims map[string]any) ([]any, error) {
	out := make([]any, len(c.Params))
	for i, p := range c.Params {
		if p.Claim == "" {
			out[i] = p.Literal
			continue
		}
		v, ok := claims[p.Claim]
		if !ok {
			return nil, fmt.Errorf("missing token claim %q", p.Claim)
		}
		out[i] = v
	}
	return out, nil
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/policy/ -v`
Expected: 全部 PASS(含注入语料)

- [ ] **Step 5: Commit**

```bash
git add internal/policy
git commit -m "feat(policy): compile AST to parameterized SQL; injection corpus gate"
```

### Task 5: policy 包——native fuzzing

**Files:**
- Test: `internal/policy/fuzz_test.go`

- [ ] **Step 1: 写 fuzz 测试**

`internal/policy/fuzz_test.go`:

```go
package policy

import "testing"

// FuzzParseCompile asserts the parse→compile pipeline never panics and,
// when it succeeds, always yields SQL built only from quoted lowercase
// identifiers, fixed keywords, and $N placeholders.
func FuzzParseCompile(f *testing.F) {
	seeds := []string{
		"org_id == $token.org",
		"a in ('x','y') and not b is null",
		"true or false",
		"(a == 1 or b >= 2) and c < 3",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		e, err := Parse(src)
		if err != nil {
			return
		}
		c, err := Compile(e)
		if err != nil {
			return
		}
		for _, r := range c.SQL {
			switch {
			case r >= 'a' && r <= 'z':
			case r >= 'A' && r <= 'Z':
			case r >= '0' && r <= '9':
			case r == '_' || r == '"' || r == '$' || r == '(' || r == ')':
			case r == ' ' || r == ',' || r == '=' || r == '<' || r == '>' || r == '.':
			default:
				t.Fatalf("unexpected rune %q in SQL %q from src %q", r, c.SQL, src)
			}
		}
	})
}
```

- [ ] **Step 2: 跑短 fuzz 确认不炸**

Run: `go test ./internal/policy/ -fuzz FuzzParseCompile -fuzztime 30s`
Expected: 30 秒内无 crash(出现 `fuzz: elapsed` 行持续输出,退出码 0)

- [ ] **Step 3: 把 fuzz 语料回归跑一遍(常驻测试)**

Run: `go test ./internal/policy/ -v`
Expected: FuzzParseCompile 作为 seed 回归 PASS

- [ ] **Step 4: Commit**

```bash
git add internal/policy
git commit -m "test(policy): native fuzz for parse+compile pipeline"
```

### Task 6: introspect 包——列内省(集成, testcontainers)

**Files:**
- Create: `internal/introspect/introspect.go`
- Create: `test/integration/pg.go`
- Create: `test/integration/fixture.sql`
- Test: `internal/introspect/columns_integration_test.go`

- [ ] **Step 1: 写 fixture 与 harness**

`test/integration/fixture.sql`(SilkLine 形态最小样本):

```sql
CREATE TABLE organizations (
  id   bigint PRIMARY KEY,
  name text NOT NULL
);
CREATE TABLE buyers (
  id    bigint PRIMARY KEY,
  org_id bigint NOT NULL REFERENCES organizations(id),
  name  text NOT NULL
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
```

`test/integration/pg.go`:

```go
// Package integration provides a throwaway PostgreSQL via testcontainers.
// Tests skip (not fail) when Docker is unavailable.
package integration

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func StartPG(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	pgc, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("plinth"),
		postgres.WithUsername("plinth"),
		postgres.WithPassword("plinth"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Skipf("docker unavailable: %v", err)
	}
	t.Cleanup(func() { _ = pgc.Terminate(ctx) })
	csn, err := pgc.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, csn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// ApplyFixture runs schema/fixture.sql on the pool.
func ApplyFixture(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx, contextCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer contextCancel()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, fixtureSQL); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

// ConnectDirect returns a non-pool connection for callers needing pgx.Conn.
func ConnectDirect(t *testing.T, pool *pgxpool.Pool) *pgx.Conn {
	t.Helper()
	c, err := pgx.Connect(context.Background(), pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c
}
```

fixture 以 Go 常量嵌入(harness 同包),新建 `test/integration/fixture.go`:

```go
package integration

const fixtureSQL = `...上面 fixture.sql 全文...`
```

(将 fixture.sql 全文复制进该常量;`fixture.sql` 文件本身作为文档留档,构建不引用。)

- [ ] **Step 2: 写失败测试**

`internal/introspect/columns_integration_test.go`:

```go
//go:build integration

package introspect_test

import (
	"testing"

	"github.com/dayuer/plinth/internal/introspect"
	"github.com/dayuer/plinth/test/integration"
)

func TestSnapshotTablesColumns(t *testing.T) {
	pool := integration.StartPG(t)
	integration.ApplyFixture(t, pool)

	snap, err := introspect.SnapshotTables(t.Context(), pool,
		[2]string{"public", "invoices"},
		[2]string{"public", "buyers"},
		[2]string{"public", "organizations"},
	)
	if err != nil {
		t.Fatal(err)
	}
	inv := snap.Tables["public.invoices"]
	if inv == nil {
		t.Fatal("invoices not in snapshot")
	}
	if len(inv.Columns) != 9 {
		t.Fatalf("columns = %d, want 9: %+v", len(inv.Columns), inv.Columns)
	}
	byName := map[string]Column{}
	for _, c := range inv.Columns {
		byName[c.Name] = c
	}
	if byName["org_id"].Nullable {
		t.Error("org_id should be NOT NULL")
	}
	if !byName["currency"].Nullable {
		t.Error("currency should be nullable")
	}
	if byName["amount_total"].Type != "numeric" {
		t.Errorf("amount_total type = %s", byName["amount_total"].Type)
	}
}

func TestSnapshotTablesMissing(t *testing.T) {
	pool := integration.StartPG(t)
	integration.ApplyFixture(t, pool)
	_, err := introspect.SnapshotTables(t.Context(), pool, [2]string{"public", "nope"})
	if err == nil {
		t.Fatal("expected error for missing table")
	}
}
```

- [ ] **Step 3: 跑测试确认失败**

Run: `go test -tags integration ./internal/introspect/ -v`
Expected: FAIL(`undefined: introspect.SnapshotTables` 等)

- [ ] **Step 4: 写实现**

`internal/introspect/introspect.go`:

```go
// Package introspect reads the live schema of an existing PostgreSQL
// database. It never writes anything (spec §2: zero DDL).
package introspect

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Column struct {
	Name     string
	Type     string // information_schema data_type
	Nullable bool
}

func (c Column) IsBoolean() bool { return c.Type == "boolean" }

type FK struct {
	Column    string
	RefSchema string
	RefTable  string
	RefColumn string
}

type Table struct {
	Schema  string
	Name    string
	Columns []Column
	FKs     []FK
}

type Snapshot struct {
	Tables map[string]*Table // key "schema.table"
}

func Key(schema, table string) string { return schema + "." + table }

// SnapshotTables introspects the given tables. Missing tables are an error.
func SnapshotTables(ctx context.Context, pool *pgxpool.Pool, keys ...[2]string) (*Snapshot, error) {
	snap := &Snapshot{Tables: map[string]*Table{}}
	for _, k := range keys {
		t, err := snapshotTable(ctx, pool, k[0], k[1])
		if err != nil {
			return nil, err
		}
		snap.Tables[Key(k[0], k[1])] = t
	}
	return snap, nil
}

const colSQL = `
SELECT column_name, data_type, is_nullable
FROM information_schema.columns
WHERE table_schema = $1 AND table_name = $2
ORDER BY ordinal_position`

const fkSQL = `
SELECT a.attname, af.attname, con.confrelid::regclass::text
FROM pg_constraint con
JOIN pg_attribute a  ON a.attrelid  = con.conrelid AND a.attnum = con.conkey[1]
JOIN pg_attribute af ON af.attrelid = con.confrelid AND af.attnum = con.confkey[1]
WHERE con.contype = 'f'
  AND cardinality(con.conkey) = 1
  AND cardinality(con.confkey) = 1
  AND con.conrelid = ($1 || '.' || $2)::regclass
ORDER BY a.attname`

func snapshotTable(ctx context.Context, pool *pgxpool.Pool, schema, table string) (*Table, error) {
	t := &Table{Schema: schema, Name: table}

	rows, err := pool.Query(ctx, colSQL, schema, table)
	if err != nil {
		return nil, fmt.Errorf("introspect %s: %w", Key(schema, table), err)
	}
	defer rows.Close()
	for rows.Next() {
		var c Column
		var nullable string
		if err := rows.Scan(&c.Name, &c.Type, &nullable); err != nil {
			return nil, fmt.Errorf("introspect %s: %w", Key(schema, table), err)
		}
		c.Nullable = nullable == "YES"
		t.Columns = append(t.Columns, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("introspect %s: %w", Key(schema, table), err)
	}
	if len(t.Columns) == 0 {
		return nil, fmt.Errorf("introspect %s: table or view not found", Key(schema, table))
	}
	return t, nil
}
```

- [ ] **Step 5: 跑测试确认通过**

Run: `go get github.com/jackc/pgx/v5 github.com/testcontainers/testcontainers-go/modules/postgres && go test -tags integration ./internal/introspect/ -v`
Expected: 2 个集成测试 PASS(无 Docker 环境显示 SKIP)

- [ ] **Step 6: Commit**

```bash
git add internal/introspect test go.mod go.sum
git commit -m "feat(introspect): information_schema column introspection + testcontainers harness"
```

### Task 7: introspect 包——外键内省

**Files:**
- Modify: `internal/introspect/introspect.go`(snapshotTable 增加 FK 查询)
- Test: `internal/introspect/fks_integration_test.go`

- [ ] **Step 1: 写失败测试**

`internal/introspect/fks_integration_test.go`:

```go
//go:build integration

package introspect_test

import (
	"testing"

	"github.com/dayuer/plinth/internal/introspect"
	"github.com/dayuer/plinth/test/integration"
)

func TestSnapshotTablesFKs(t *testing.T) {
	pool := integration.StartPG(t)
	integration.ApplyFixture(t, pool)

	snap, err := introspect.SnapshotTables(t.Context(), pool,
		[2]string{"public", "invoices"},
		[2]string{"public", "buyers"},
	)
	if err != nil {
		t.Fatal(err)
	}
	inv := snap.Tables["public.invoices"]
	fk := map[string]introspect.FK{}
	for _, f := range inv.FKs {
		fk[f.Column] = f
	}
	if len(inv.FKs) != 2 {
		t.Fatalf("invoices FKs = %+v, want 2", inv.FKs)
	}
	if fk["buyer_id"].RefTable != "buyers" || fk["buyer_id"].RefColumn != "id" || fk["buyer_id"].RefSchema != "public" {
		t.Errorf("buyer_id FK = %+v", fk["buyer_id"])
	}
	if fk["org_id"].RefTable != "organizations" {
		t.Errorf("org_id FK = %+v", fk["org_id"])
	}
	if len(snap.Tables["public.buyers"].FKs) != 1 {
		t.Errorf("buyers FKs = %+v", snap.Tables["public.buyers"].FKs)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test -tags integration ./internal/introspect/ -run FKs -v`
Expected: FAIL(FKs 恒为空)

- [ ] **Step 3: 在 snapshotTable 末尾(`return t, nil` 之前)加 FK 查询**

在 `internal/introspect/introspect.go` 的 `snapshotTable` 中追加(`fkSQL` 常量已随 Task 6 写入):

```go
	fkRows, err := pool.Query(ctx, fkSQL, schema, table)
	if err != nil {
		return nil, fmt.Errorf("introspect %s fks: %w", Key(schema, table), err)
	}
	defer fkRows.Close()
	for fkRows.Next() {
		var f FK
		var ref string
		if err := fkRows.Scan(&f.Column, &f.RefColumn, &ref); err != nil {
			return nil, fmt.Errorf("introspect %s fks: %w", Key(schema, table), err)
		}
		if i := indexByte(ref, '.'); i >= 0 {
			f.RefSchema, f.RefTable = ref[:i], ref[i+1:]
		} else {
			f.RefSchema, f.RefTable = "public", ref
		}
		t.FKs = append(t.FKs, f)
	}
	if err := fkRows.Err(); err != nil {
		return nil, fmt.Errorf("introspect %s fks: %w", Key(schema, table), err)
	}
```

同文件加 helper:

```go
func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test -tags integration ./internal/introspect/ -v`
Expected: 列 + FK 测试全 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/introspect
git commit -m "feat(introspect): single-column FK introspection via pg_catalog"
```

### Task 8: catalog 包——交叉校验(结构 + 表达式)

**Files:**
- Create: `internal/catalog/catalog.go`
- Test: `internal/catalog/catalog_test.go`

校验规则(spec §3/§4):model 表必须在快照中;`columns.hide`、关系 `on` 列必须存在;关系引用表必须存在于快照;policy 必须指向已暴露表;`columns.allow` 的 `"*"` 只能单独出现、deny 必须存在且 ⊆ 全列;`row` 表达式必须可解析可编译、引用列必须存在、裸列必须为 boolean、claims ⊆ 配置白名单;`role: "*"` 必须存在(默认拒绝兜底)。

- [ ] **Step 1: 写失败测试**

`internal/catalog/catalog_test.go`:

```go
package catalog

import (
	"strings"
	"testing"

	"github.com/dayuer/plinth/internal/introspect"
	"github.com/dayuer/plinth/internal/meta"
)

func testSnapshot() *introspect.Snapshot {
	inv := &introspect.Table{Schema: "public", Name: "invoices", Columns: []introspect.Column{
		{Name: "id", Type: "bigint"},
		{Name: "org_id", Type: "bigint"},
		{Name: "buyer_id", Type: "bigint", Nullable: true},
		{Name: "status", Type: "text"},
		{Name: "active", Type: "boolean"},
		{Name: "internal_note", Type: "text", Nullable: true},
	}}
	inv.FKs = []introspect.FK{{
		Column: "buyer_id", RefSchema: "public", RefTable: "buyers", RefColumn: "id",
	}}
	buyers := &introspect.Table{Schema: "public", Name: "buyers", Columns: []introspect.Column{
		{Name: "id", Type: "bigint"},
	}}
	return &introspect.Snapshot{Tables: map[string]*introspect.Table{
		"public.invoices": inv,
		"public.buyers":   buyers,
	}}
}

func invoiceModel() meta.Model {
	return meta.Model{
		Schema: "public", Table: "invoices", Expose: true,
		Columns: meta.Columns{Hide: []string{"internal_note"}},
		Relations: []meta.Relation{{
			Name: "buyer", Type: "many-to-one", On: "buyer_id",
			References: meta.Ref{Table: "buyers", Column: "id"}, Expose: true,
		}},
	}
}

func TestBuildOK(t *testing.T) {
	policies := []meta.Policy{{
		Schema: "public", Table: "invoices",
		Rules: []meta.Rule{
			{Role: "accountant", Columns: meta.ColumnRules{Allow: []string{"*"}, Deny: []string{"internal_note"}},
				Row: "org_id == $token.org and status != 'VOID'"},
			{Role: "ops", Columns: meta.ColumnRules{Allow: []string{"id", "status", "buyer_id", "active"}}, Row: "active"},
			{Role: "*", Columns: meta.ColumnRules{Allow: []string{}}, Row: "false"},
		},
	}}
	_, errs := Build([]meta.Model{invoiceModel()}, policies, testSnapshot(), []string{"org", "role"})
	for _, e := range errs {
		t.Errorf("unexpected error: %v", e)
	}
	if len(errs) > 0 {
		t.FailNow()
	}
}

func wantErrContaining(t *testing.T, errs []error, frag string) {
	t.Helper()
	for _, e := range errs {
		if strings.Contains(e.Error(), frag) {
			return
		}
	}
	t.Fatalf("errors %+v: want one containing %q", errs, frag)
}

func TestBuildStructuralErrors(t *testing.T) {
	m := invoiceModel()
	m.Table = "ghost"
	_, errs := Build([]meta.Model{m}, nil, testSnapshot(), nil)
	wantErrContaining(t, errs, "not found")

	m = invoiceModel()
	m.Columns.Hide = []string{"no_such_column"}
	_, errs = Build([]meta.Model{m}, nil, testSnapshot(), nil)
	wantErrContaining(t, errs, "no_such_column")

	m = invoiceModel()
	m.Relations[0].On = "wrong_col"
	_, errs = Build([]meta.Model{m}, nil, testSnapshot(), nil)
	wantErrContaining(t, errs, "wrong_col")

	m = invoiceModel()
	m.Relations[0].References.Table = "ghosts"
	_, errs = Build([]meta.Model{m}, nil, testSnapshot(), nil)
	wantErrContaining(t, errs, "ghosts")

	m = invoiceModel()
	m.Relations[0].Type = "has-many"
	_, errs = Build([]meta.Model{m}, nil, testSnapshot(), nil)
	wantErrContaining(t, errs, "many-to-one")
}

func TestBuildPolicyErrors(t *testing.T) {
	mk := func(rules ...meta.Rule) []meta.Policy {
		return []meta.Policy{{Schema: "public", Table: "invoices", Rules: rules}}
	}
	base := func(row string, allow, deny []string) meta.Rule {
		return meta.Rule{Role: "r", Columns: meta.ColumnRules{Allow: allow, Deny: deny}, Row: row}
	}
	cols := []string{"id", "status"}

	_, errs := Build([]meta.Model{invoiceModel()}, mk(base("org_id == $token.org", cols, nil)), testSnapshot(), []string{"role"})
	wantErrContaining(t, errs, "claim \"org\" not in allowlist")

	_, errs = Build([]meta.Model{invoiceModel()}, mk(base("nope == 1", cols, nil)), testSnapshot(), nil)
	wantErrContaining(t, errs, "nope")

	_, errs = Build([]meta.Model{invoiceModel()}, mk(base("status", cols, nil)), testSnapshot(), nil)
	wantErrContaining(t, errs, "boolean")

	_, errs = Build([]meta.Model{invoiceModel()}, mk(base("row: broken (", cols, nil)), testSnapshot(), nil)
	wantErrContaining(t, errs, "row:")

	_, errs = Build([]meta.Model{invoiceModel()}, mk(base("false", []string{"*", "id"}, nil)), testSnapshot(), nil)
	wantErrContaining(t, errs, `"*" alone`)

	_, errs = Build([]meta.Model{invoiceModel()}, mk(base("false", cols, []string{"ghost"})), testSnapshot(), nil)
	wantErrContaining(t, errs, "ghost")

	_, errs = Build([]meta.Model{invoiceModel()}, mk(base("false", cols, nil)), testSnapshot(), nil)
	wantErrContaining(t, errs, `"*" catch-all`) // missing role "*"

	p := meta.Policy{Schema: "public", Table: "ghost", Rules: []meta.Rule{{Role: "*", Columns: meta.ColumnRules{Allow: nil}, Row: "false"}}}
	_, errs = Build([]meta.Model{invoiceModel()}, []meta.Policy{p}, testSnapshot(), nil)
	wantErrContaining(t, errs, "not exposed")
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/catalog/ -v`
Expected: FAIL(`undefined: Build`)

- [ ] **Step 3: 写实现**

`internal/catalog/catalog.go`:

```go
// Package catalog merges introspected tables, model annotations and
// policies, and cross-validates every reference between them.
package catalog

import (
	"fmt"
	"sort"

	"github.com/dayuer/plinth/internal/introspect"
	"github.com/dayuer/plinth/internal/meta"
	"github.com/dayuer/plinth/internal/policy"
)

type Entry struct {
	Table  *introspect.Table
	Model  *meta.Model
	Policy *meta.Policy
}

type Catalog struct {
	Entries map[string]*Entry // key "schema.table"
}

func (c *Catalog) Keys() []string {
	keys := make([]string, 0, len(c.Entries))
	for k := range c.Entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Build cross-validates metadata against the snapshot. It returns all
// problems found (never aborts on first) so `validate` prints a full report.
func Build(models []meta.Model, ps []meta.Policy, snap *introspect.Snapshot, claimAllowlist []string) (*Catalog, []error) {
	var errs []error
	c := &Catalog{Entries: map[string]*Entry{}}

	for _, m := range models {
		if !m.Expose {
			continue
		}
		if m.Schema == "" {
			m.Schema = "public"
		}
		key := introspect.Key(m.Schema, m.Table)
		t, ok := snap.Tables[key]
		if !ok {
			errs = append(errs, fmt.Errorf("model %s: table not found in database", key))
			continue
		}
		e := &Entry{Table: t, Model: &m}
		c.Entries[key] = e

		cols := map[string]bool{}
		for _, col := range t.Columns {
			cols[col.Name] = true
		}

		for _, h := range m.Columns.Hide {
			if !cols[h] {
				errs = append(errs, fmt.Errorf("model %s: hide column %q does not exist", key, h))
			}
		}
		for _, r := range m.Relations {
			if r.Type != "many-to-one" {
				errs = append(errs, fmt.Errorf("model %s relation %s: only many-to-one is supported", key, r.Name))
			}
			if !cols[r.On] {
				errs = append(errs, fmt.Errorf("model %s relation %s: column %q does not exist", key, r.Name, r.On))
			}
			refKey := introspect.Key(r.Schema(), r.References.Table)
			if _, ok := snap.Tables[refKey]; !ok {
				errs = append(errs, fmt.Errorf("model %s relation %s: referenced table %s not introspected", key, r.Name, refKey))
			}
		}
	}

	allowed := map[string]bool{}
	for _, a := range claimAllowlist {
		allowed[a] = true
	}

	for _, p := range ps {
		if p.Schema == "" {
			p.Schema = "public"
		}
		key := introspect.Key(p.Schema, p.Table)
		e, ok := c.Entries[key]
		if !ok {
			errs = append(errs, fmt.Errorf("policy %s: table not exposed by any model", key))
			continue
		}
		e.Policy = &p

		cols := map[string]introspect.Column{}
		for _, col := range e.Table.Columns {
			cols[col.Name] = col
		}

		hasCatchAll := false
		for _, r := range p.Rules {
			if r.Role == "*" {
				hasCatchAll = true
			}
			errs = append(errs, validateColumns(key, r, cols)...)
			errs = append(errs, validateRow(key, r, cols, allowed)...)
		}
		if !hasCatchAll {
			errs = append(errs, fmt.Errorf("policy %s: missing \"*\" catch-all rule (default-deny floor)", key))
		}
	}
	return c, errs
}

func validateColumns(key string, r meta.Rule, cols map[string]introspect.Column) []error {
	var errs []error
	allow := r.Columns.Allow
	if len(allow) == 1 && allow[0] == "*" {
		if r.Columns.Deny == nil {
			errs = append(errs, fmt.Errorf("policy %s role %s: allow \"*\" must list explicit deny columns", key, r.Role))
		}
	} else {
		for _, a := range allow {
			if a == "*" {
				errs = append(errs, fmt.Errorf("policy %s role %s: \"*\" must appear alone", key, r.Role))
				break
			}
			if _, ok := cols[a]; !ok {
				errs = append(errs, fmt.Errorf("policy %s role %s: allow column %q does not exist", key, r.Role, a))
			}
		}
	}
	for _, d := range r.Columns.Deny {
		if d == "*" {
			continue
		}
		if _, ok := cols[d]; !ok {
			errs = append(errs, fmt.Errorf("policy %s role %s: deny column %q does not exist", key, r.Role, d))
		}
	}
	return errs
}

func validateRow(key string, r meta.Rule, cols map[string]introspect.Column, allowed map[string]bool) []error {
	var errs []error
	e, err := policy.Parse(r.Row)
	if err != nil {
		return append(errs, fmt.Errorf("policy %s role %s: row: %v", key, r.Role, err))
	}
	if _, err := policy.Compile(e); err != nil {
		return append(errs, fmt.Errorf("policy %s role %s: row: %v", key, r.Role, err))
	}
	for _, name := range policy.ColumnsUsed(e) {
		col, ok := cols[name]
		if !ok {
			errs = append(errs, fmt.Errorf("policy %s role %s: row references unknown column %q", key, r.Role, name))
			continue
		}
		if exprIsBareColumn(e, name) && !col.IsBoolean() {
			errs = append(errs, fmt.Errorf("policy %s role %s: bare column %q must be boolean", key, r.Role, name))
		}
	}
	for _, name := range policy.ClaimsUsed(e) {
		if !allowed[name] {
			errs = append(errs, fmt.Errorf("policy %s role %s: claim %q not in allowlist", key, r.Role, name))
		}
	}
	return errs
}

func exprIsBareColumn(e policy.Expr, name string) bool {
	switch n := e.(type) {
	case policy.Column:
		return n.Name == name
	case policy.Not:
		return exprIsBareColumn(n.X, name)
	case policy.Binary:
		return exprIsBareColumn(n.L, name) || exprIsBareColumn(n.R, name)
	}
	return false
}
```

`internal/meta/types.go` 的 `Relation` 补一个默认 schema 方法(引用表默认与宿主同 schema):

```go
// Schema returns the referenced table's schema, defaulting to the
// relation's own default "public".
func (r Relation) Schema() string { return "public" }
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/catalog/ -v`
Expected: TestBuildOK PASS,两个错误路径测试 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/catalog internal/meta
git commit -m "feat(catalog): cross-validate models/policies against live introspection"
```

### Task 9: cli validate

**Files:**
- Modify: `internal/cli/run.go`
- Create: `internal/cli/validate.go`
- Test: `internal/cli/validate_test.go`

- [ ] **Step 1: 写失败测试**

`internal/cli/validate_test.go`:

```go
package cli

import (
	"os"
	"path/filepath"
	"testing"
)

const okCfg = `database:
  url: postgres://x
auth:
  jwks_url: https://x
  roles_claim: role
  claims: [sub, org, role]
`

const okModel = `schema: public
table: invoices
expose: true
`

const okPolicy = `schema: public
table: invoices
rules:
  - role: "*"
    columns: { allow: [] }
    row: "false"
`

func writeMeta(t *testing.T, policyBody string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{
		"plinth.yml":            okCfg,
		"models/invoices.yml":   okModel,
		"policies/invoices.yml": policyBody,
	} {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestValidateOK(t *testing.T) {
	dir := writeMeta(t, okPolicy)
	if err := Run([]string{"validate", "--dir", dir}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateBadExpression(t *testing.T) {
	dir := writeMeta(t, `schema: public
table: invoices
rules:
  - role: "*"
    columns: { allow: [] }
    row: "org_id =="
`)
	err := Run([]string{"validate", "--dir", dir})
	if err == nil {
		t.Fatal("expected error")
	}
	if code := ExitCode(err); code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}

func TestValidateClaimNotAllowlisted(t *testing.T) {
	dir := writeMeta(t, `schema: public
table: invoices
rules:
  - role: "*"
    columns: { allow: [] }
    row: "id == $token.ghost"
`)
	err := Run([]string{"validate", "--dir", dir})
	if err == nil {
		t.Fatal("expected error")
	}
	if code := ExitCode(err); code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/cli/ -run TestValidate -v`
Expected: FAIL(validate not implemented)

- [ ] **Step 3: 写实现**

`internal/cli/validate.go`:

```go
package cli

import (
	"flag"
	"fmt"
	"os"

	"github.com/dayuer/plinth/internal/meta"
	"github.com/dayuer/plinth/internal/policy"
)

// runValidate loads and validates metadata without touching a database.
// Syntactic + expression checks run here; referential checks need the DB
// and belong to runDiff.
func runValidate(args []string) error {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	dir := fs.String("dir", ".", "metadata directory")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, models, policies, err := meta.LoadDir(*dir)
	if err != nil {
		return &MetaError{Err: err}
	}

	var errs []error
	for _, p := range policies {
		if p.Schema == "" {
			p.Schema = "public"
		}
		for _, r := range p.Rules {
			e, err := policy.Parse(r.Row)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s.%s role %s: row: %v", p.Schema, p.Table, r.Role, err))
				continue
			}
			if _, err := policy.Compile(e); err != nil {
				errs = append(errs, fmt.Errorf("%s.%s role %s: row: %v", p.Schema, p.Table, r.Role, err))
			}
			for _, name := range policy.ClaimsUsed(e) {
				if !contains(cfg.Auth.Claims, name) {
					errs = append(errs, fmt.Errorf("%s.%s role %s: claim %q not in allowlist", p.Schema, p.Table, r.Role, name))
				}
			}
		}
	}
	if len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintln(os.Stderr, "validate:", e)
		}
		return &MetaError{Err: fmt.Errorf("%d metadata problems", len(errs))}
	}
	fmt.Println("validate: ok")
	return nil
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
```

`internal/cli/run.go` 的 switch 改为:

```go
	switch args[0] {
	case "validate":
		return runValidate(args[1:])
	case "diff":
		return &MetaError{Err: fmt.Errorf("diff: not implemented yet (Plan 1 Task 10)")}
	case "serve":
		return &MetaError{Err: fmt.Errorf("serve: not implemented yet (Plan 2)")}
	default:
		usage()
		return fmt.Errorf("unknown command %q", args[0])
	}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/cli/ -v && go build ./...`
Expected: TestValidateOK / TestValidateBadExpression PASS(后者退出码 2)

- [ ] **Step 5: Commit**

```bash
git add internal/cli
git commit -m "feat(cli): plinth validate — offline metadata and expression checks"
```

### Task 10: cli diff(连库交叉校验/漂移检测)

**Files:**
- Modify: `internal/cli/run.go`
- Create: `internal/cli/diff.go`
- Test: `internal/cli/diff_integration_test.go`

- [ ] **Step 1: 写失败测试**

`internal/cli/diff_integration_test.go`:

```go
//go:build integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dayuer/plinth/test/integration"
)

func writeFullMeta(t *testing.T, dir string) {
	t.Helper()
	files := map[string]string{
		"plinth.yml": okCfg,
		"models/invoices.yml": `schema: public
table: invoices
expose: true
columns:
  hide: [internal_note]
`,
		"policies/invoices.yml": `schema: public
table: invoices
rules:
  - role: accountant
    columns: { allow: "*" , deny: [internal_note] }
    row: org_id == $token.org
  - role: "*"
    columns: { allow: [] }
    row: "false"
`,
	}
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDiffOK(t *testing.T) {
	pool := integration.StartPG(t)
	integration.ApplyFixture(t, pool)
	dir := t.TempDir()
	writeFullMeta(t, dir)
	t.Setenv("PLINTH_DATABASE_URL", pool.Config().ConnString())
	// okCfg's url is ${TEST_DATABASE_URL}; point it at the container
	os.WriteFile(filepath.Join(dir, "plinth.yml"), []byte("database:\n  url: "+pool.Config().ConnString()+"\nauth:\n  jwks_url: https://x\n  roles_claim: role\n  claims: [sub, org, role]\n"), 0o644)

	if err := Run([]string{"diff", "--dir", dir}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDiffDrift(t *testing.T) {
	pool := integration.StartPG(t)
	integration.ApplyFixture(t, pool)
	_, _ = pool.Exec(t.Context(), `ALTER TABLE invoices DROP COLUMN internal_note`)
	dir := t.TempDir()
	writeFullMeta(t, dir)
	os.WriteFile(filepath.Join(dir, "plinth.yml"), []byte("database:\n  url: "+pool.Config().ConnString()+"\nauth:\n  jwks_url: https://x\n  roles_claim: role\n  claims: [sub, org, role]\n"), 0o644)

	err := Run([]string{"diff", "--dir", dir})
	if err == nil {
		t.Fatal("expected drift error")
	}
	if code := ExitCode(err); code != 3 {
		t.Errorf("exit code = %d, want 3", code)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test -tags integration ./internal/cli/ -run TestDiff -v`
Expected: FAIL(diff not implemented)

- [ ] **Step 3: 写实现**

`internal/cli/diff.go`:

```go
package cli

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/dayuer/plinth/internal/catalog"
	"github.com/dayuer/plinth/internal/introspect"
	"github.com/dayuer/plinth/internal/meta"
	"github.com/jackc/pgx/v5/pgxpool"
)

// runDiff = validate + live introspection + referential cross-check.
// Exit 2 on metadata problems, 3 on database/drift problems (spec §7).
func runDiff(args []string) error {
	fs := flag.NewFlagSet("diff", flag.ContinueOnError)
	dir := fs.String("dir", ".", "metadata directory")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, models, policies, err := meta.LoadDir(*dir)
	if err != nil {
		return &MetaError{Err: err}
	}

	pool, err := pgxpool.New(context.Background(), cfg.Database.URL)
	if err != nil {
		return &DBError{Err: fmt.Errorf("connect: %w", err)}
	}
	defer pool.Close()

	keys := make([][2]string, 0, len(models))
	seen := map[string]bool{}
	for _, m := range models {
		if !m.Expose {
			continue
		}
		if m.Schema == "" {
			m.Schema = "public"
		}
		k := introspect.Key(m.Schema, m.Table)
		if !seen[k] {
			seen[k] = true
			keys = append(keys, [2]string{m.Schema, m.Table})
		}
	}

	snap, err := introspect.SnapshotTables(context.Background(), pool, keys...)
	if err != nil {
		return &DBError{Err: err}
	}

	_, errs := catalog.Build(models, policies, snap, cfg.Auth.Claims)
	if len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintln(os.Stderr, "diff:", e)
		}
		return &DBError{Err: fmt.Errorf("%d drift/validation problems", len(errs))}
	}
	fmt.Println("diff: ok — metadata matches live database")
	return nil
}
```

`internal/cli/run.go` 的 switch 中 `case "diff":` 改为:

```go
	case "diff":
		return runDiff(args[1:])
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test -tags integration ./internal/cli/ -v && go test ./... -v`
Expected: 集成两个 PASS;全量(不带 tag)也全 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cli
git commit -m "feat(cli): plinth diff — referential drift check against live PG"
```

### Task 11: 文档同步、README 快速上手与 v0.1.0

**Files:**
- Modify: `README.md`
- Modify: `docs/specs/2026-08-16-plinth-design.md`(policies YAML 补 `schema:` 字段示例)
- Create: `CHANGELOG.md`

- [ ] **Step 1: spec 同步**

在 `docs/specs/2026-08-16-plinth-design.md` §3 的 `policies/invoices.yml` 示例首行加 `schema: public`,并在文件头「状态」行后加一句:`2026-08-16 修订:policies 增加可选 schema 字段(默认 public)。`

- [ ] **Step 2: README 增加快速上手**

在 README「文档」节之前插入:

```markdown
## Quickstart

```bash
make build
# offline check: syntax + expression compilation
./bin/plinth validate --dir path/to/metadata
# live check: introspect the database and cross-validate
DATABASE_URL=postgres://... ./bin/plinth diff --dir path/to/metadata
```

Status: foundation CLI only (`validate`/`diff`). REST gateway, events and MCP arrive in Plans 2–5.
```

- [ ] **Step 3: CHANGELOG**

`CHANGELOG.md`:

```markdown
# Changelog

## v0.1.0 (2026-08-16)

- `plinth validate`: offline metadata + row-expression validation (exit 2 on problems)
- `plinth diff`: live PostgreSQL introspection and referential drift check (exit 3 on drift)
- Row-filter expression language: parse → compile to parameterized SQL, injection corpus + native fuzzing gates
```

- [ ] **Step 4: 终验**

Run: `make lint && make test && go test -tags integration ./... -count=1`
Expected: 全绿(无 Docker 的环境集成测试 SKIP)

Run: `git tag v0.1.0`

- [ ] **Step 5: Commit 并推送**

```bash
git add README.md CHANGELOG.md docs
git commit -m "docs: quickstart, changelog, spec schema-field amendment; tag v0.1.0"
git push origin main --tags
```

---

## 后续计划(不在本计划内)

- **Plan 2** REST 读路径:JWT(JWKS)、角色解析、GET 查询流水线(过滤/分页/order/select 嵌套深度 1)、权限矩阵集成测试
- **Plan 3** 写路径:POST/PATCH/DELETE 同谓词、0 行受影响=404、SQLSTATE 映射
- **Plan 4** 事件引擎:pgoutput 复制流、webhook 投递/重试/死信(bbolt)、cron
- **Plan 5** MCP server:streamable HTTP + stdio、与 REST 同流水线、高危工具开关

## Self-Review 记录

- **Spec 覆盖**:§3 metadata(Task 2/8)、§4 表达式与安全(Task 3/4/5)、§3 diff 漂移(Task 10)、§7 退出码 2/3(Task 1/9/10)、§8 注入门禁与 fuzz(Task 4/5)。§4 REST/JWT、§5 事件、§6 MCP 属 Plan 2–5。
- **占位符扫描**:无 TBD;所有代码步骤给了全文。
- **类型一致性**:`MetaError/DBError`(Task 1)贯穿 Task 9/10;`policy.Parse/Compile/Bind/ColumnsUsed/ClaimsUsed`(Task 3/4)与 Task 8/9 用法一致;`introspect.SnapshotTables/Key/Snapshot.Tables`(Task 6/7)与 Task 8/10 一致;`meta.LoadDir` 四返回值(Task 2)与 Task 9/10 一致;`Relation.Schema()`(Task 8)在 Task 2 types 之外补充定义,Task 8 步骤内已写明落点。
