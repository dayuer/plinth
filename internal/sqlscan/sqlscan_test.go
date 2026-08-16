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

func TestDollarQuoteIdentifierAdjacency(t *testing.T) {
	// PostgreSQL lexes '$' as an identifier-continue byte: a$$b is ONE
	// identifier, not 'a' + dollar-quote + 'b'. '$$' must open a dollar
	// quote only when the preceding byte cannot be part of an identifier.
	// Without that guard this attack string blanks "; DROP TABLE t;" from
	// Analyze output — a readcheck bypass.
	attack := "SELECT a$$b; DROP TABLE t; SELECT x$$y, $$ $$, $$ $$"
	clean, params, err := Analyze(attack)
	if err != nil {
		t.Fatalf("Analyze(attack): %v", err)
	}
	if !strings.Contains(clean, ";") || !strings.Contains(clean, "DROP") {
		t.Errorf("code blanked by false dollar quote: %q", clean)
	}
	if len(params) != 0 {
		t.Errorf("params = %v", params)
	}
	out, order, err := Rewrite(attack)
	if err != nil {
		t.Fatalf("Rewrite(attack): %v", err)
	}
	if out != attack || len(order) != 0 {
		t.Errorf("Rewrite not pass-through: out=%q order=%v", out, order)
	}

	// $$$ after an identifier byte: three ordinary '$' bytes, not an
	// unterminated dollar quote (the old code errored on this).
	src := "SELECT x$$$y"
	clean, _, err = Analyze(src)
	if err != nil {
		t.Fatalf("Analyze(%q): %v", src, err)
	}
	if !strings.Contains(clean, "$$$") {
		t.Errorf("$$$ not visible as code: %q", clean)
	}

	// Non-ASCII bytes are identifier bytes in PG too (é is two UTF-8
	// bytes, both >= 0x80).
	src = "SELECT é$$b; DELETE FROM t; SELECT c$$d"
	clean, _, err = Analyze(src)
	if err != nil {
		t.Fatalf("Analyze(%q): %v", src, err)
	}
	if !strings.Contains(clean, ";") || !strings.Contains(clean, "DELETE") {
		t.Errorf("code blanked by false dollar quote: %q", clean)
	}

	// Legit dollar quotes (not glued to an identifier) are still blanked.
	src = "SELECT $$drop me$$"
	clean, _, err = Analyze(src)
	if err != nil {
		t.Fatalf("Analyze(%q): %v", src, err)
	}
	if strings.Contains(strings.ToUpper(clean), "DROP") {
		t.Errorf("legit dollar quote not blanked: %q", clean)
	}
}

func TestMultibytePreserved(t *testing.T) {
	src := "SELECT '发票' -- 发票注释"
	clean, params, err := Analyze(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(params) != 0 {
		t.Errorf("params = %v", params)
	}
	if len(clean) != len(src) {
		t.Fatalf("len(clean) = %d, want %d (clean=%q)", len(clean), len(src), clean)
	}
	if strings.Contains(clean, "发") || strings.Contains(clean, "注") {
		t.Errorf("literal/comment contents not blanked: %q", clean)
	}
	if !strings.HasPrefix(clean, "SELECT ") {
		t.Errorf("structure lost: %q", clean)
	}
	out, order, err := Rewrite(src)
	if err != nil {
		t.Fatal(err)
	}
	if out != src || len(order) != 0 {
		t.Errorf("Rewrite = %q, order %v; want pass-through", out, order)
	}
}

func TestLengthInvariant(t *testing.T) {
	for _, src := range []string{
		"SELECT a -- c\nFROM t WHERE s = 'lit' AND n = /* blk */ :p",
		"SELECT a$$b; DROP TABLE t; SELECT x$$y, $$ $$, $$ $$",
		"SELECT '发票' -- 发票注释\nWHERE x = :pam",
	} {
		clean, _, err := Analyze(src)
		if err != nil {
			t.Fatalf("Analyze(%q): %v", src, err)
		}
		if len(clean) != len(src) {
			t.Errorf("Analyze(%q): len(clean) = %d, want %d", src, len(clean), len(src))
		}
	}
}
