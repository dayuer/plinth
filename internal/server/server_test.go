package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

// newTestServer takes the concrete *stubRunner so a nil argument (auth
// matrix cases never reach Run) can be replaced with a fresh default.
func newTestServer(t *testing.T, run *stubRunner) (*httptest.Server, *stubRunner) {
	t.Helper()
	if run == nil {
		run = &stubRunner{}
	}
	reg := registry.NewForTest(&queryfile.Query{
		Name: "q1", Mode: "read", AllowTokens: []string{"web-bff"},
		Params: []queryfile.Param{{Name: "org", Type: "int", Required: true}},
		SQL:    "SELECT id FROM t WHERE org = :org",
	})
	aud, err := audit.Open(t.TempDir()+"/a.jsonl", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = aud.Close() })
	s := New(reg, run, map[string]string{"web-bff": "tok1", "worker": "tok2"}, aud)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts, run
}

func TestAuthMatrix(t *testing.T) {
	var stub *stubRunner
	ts, _ := newTestServer(t, stub)
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
	ts, _ := newTestServer(t, stub)
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
}

func TestNullNormalizedToAbsent(t *testing.T) {
	stub := &stubRunner{}
	ts, _ := newTestServer(t, stub)
	req, _ := http.NewRequest("POST", ts.URL+"/q/q1", strings.NewReader(`{"org":1,"extra":null}`))
	req.Header.Set("X-Plinth-Token", "tok1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if _, has := stub.gotA["extra"]; has {
		t.Errorf("null must be normalized to absent: %v", stub.gotA)
	}
}

// staticRunner is a Runner safe under concurrency (unlike stubRunner, it
// keeps no per-request state).
type staticRunner struct{}

func (staticRunner) Run(context.Context, *queryfile.Query, map[string]any) (*exec.Result, error) {
	return &exec.Result{Rows: []map[string]any{{"id": int64(1)}}}, nil
}

// TestSetRegistryConcurrent pins the reload contract: SetRegistry may swap
// the query set while requests are in flight. Whatever registry a request
// observes, it resolves its query against exactly that one — 200 or a clean
// 404, never a data race or a panic (run under -race).
func TestSetRegistryConcurrent(t *testing.T) {
	mk := func(name string) *registry.Registry {
		return registry.NewForTest(&queryfile.Query{
			Name: name, Mode: "read", AllowTokens: []string{"web-bff"},
			SQL: "SELECT 1",
		})
	}
	regA, regB := mk("qa"), mk("qb")
	s := New(regA, staticRunner{}, map[string]string{"web-bff": "tok1"}, nil)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	done := make(chan struct{})
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
				}
				for _, name := range []string{"qa", "qb"} {
					req, _ := http.NewRequest("POST", ts.URL+"/q/"+name, strings.NewReader(`{}`))
					req.Header.Set("X-Plinth-Token", "tok1")
					resp, err := http.DefaultClient.Do(req)
					if err != nil {
						t.Errorf("request during swap: %v", err)
						return
					}
					resp.Body.Close()
					// Whichever registry the request loaded, the asked-for
					// name is served 200 or cleanly 404 — never 5xx.
					if resp.StatusCode != 200 && resp.StatusCode != 404 {
						t.Errorf("name %s: status %d during swap", name, resp.StatusCode)
						return
					}
				}
			}
		}()
	}
	for i := 0; i < 2000; i++ {
		if i%2 == 0 {
			s.SetRegistry(regB)
		} else {
			s.SetRegistry(regA)
		}
	}
	close(done)
	wg.Wait()
}
