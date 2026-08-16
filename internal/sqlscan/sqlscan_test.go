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
