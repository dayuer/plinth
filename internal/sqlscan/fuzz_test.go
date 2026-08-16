package sqlscan

import "testing"

func FuzzAnalyzeRewrite(f *testing.F) {
	for _, s := range []string{
		"SELECT a -- delete from x\nFROM t WHERE s = 'insert into' AND n = /* update */ 1",
		"SELECT $$dollar :notparam$$, 'it''s :fine' WHERE x = :p",
		"SELECT a$$b; DROP TABLE t; SELECT x$$y, $$ $$, $$ $$",
		"SELECT :a::text, :b in (1,2), :c is null",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		clean, params, err := Analyze(src)
		if err != nil {
			if clean != "" || params != nil {
				t.Fatalf("partial results on error: %q %v", clean, params)
			}
			return
		}
		if len(clean) != len(src) {
			t.Fatalf("length changed: %d -> %d", len(src), len(clean))
		}
		out, order, err2 := Rewrite(src)
		if err2 != nil {
			t.Fatalf("Analyze ok but Rewrite failed: %v", err2)
		}
		if len(order) != len(params) {
			t.Fatalf("param count mismatch: Analyze %v Rewrite %v", params, order)
		}
		for i := range order {
			if order[i] != params[i] {
				t.Fatalf("param order mismatch at %d", i)
			}
		}
		_ = out
	})
}
