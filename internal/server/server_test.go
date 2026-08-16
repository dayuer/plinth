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
