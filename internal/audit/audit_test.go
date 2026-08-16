package audit

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
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
		TS:     time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC),
		Caller: "web-bff", Query: "invoice-list",
		Params: map[string]any{"org_id": float64(1), "tax_id": "NPWP123", "buyer_name": "Acme"},
		Rows:   3, Ms: 12, Status: "ok",
	}
	if err := w.Record(rec); err != nil {
		t.Fatal(err)
	}
	rec.Err, rec.Status = "boom", "error"
	rec.Params = map[string]any{"tax_id": "NPWP456"}
	if err := w.Record(rec); err != nil {
		t.Fatal(err)
	}
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

func TestRecordNilParamsOmitted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exec.jsonl")
	w, err := Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Record(Record{TS: time.Now(), Caller: "c", Query: "q", Status: "ok"}); err != nil {
		t.Fatal(err)
	}
	w.Close()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) == "" || b[len(b)-1] != '\n' {
		t.Fatalf("must be newline-terminated: %q", string(b))
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if _, has := m["params"]; has {
		t.Error("nil params must be omitted, not null")
	}
}

func TestRecordConcurrent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit", "executions.jsonl")
	w, err := Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	const goroutines = 10
	const perGoroutine = 100
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(caller string) {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				if err := w.Record(Record{TS: time.Now(), Caller: caller, Query: "q", Status: "ok"}); err != nil {
					t.Errorf("Record: %v", err)
					return
				}
			}
		}("gopher")
	}
	wg.Wait()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	lines := 0
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("line %d not valid JSON: %v", lines+1, err)
		}
		lines++
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if lines != goroutines*perGoroutine {
		t.Fatalf("lines = %d, want %d", lines, goroutines*perGoroutine)
	}
}
