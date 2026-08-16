// Package server is the HTTP BFF: POST /q/{name} with static token auth,
// RFC 7807 problem-details errors, and an execution audit hook. Audit
// failures are logged and never fail the HTTP request.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/dayuer/plinth/internal/audit"
	"github.com/dayuer/plinth/internal/exec"
	"github.com/dayuer/plinth/internal/queryfile"
	"github.com/dayuer/plinth/internal/registry"
)

// Runner is the execution backend — *exec.Engine in production, a stub in
// tests.
type Runner interface {
	Run(context.Context, *queryfile.Query, map[string]any) (*exec.Result, error)
}

// maxBodyBytes caps the request body at 1MB: the first JSON value must
// complete within the limit; trailing bytes after a complete first value
// are ignored (not rejected).
const maxBodyBytes = 1 << 20

type Server struct {
	// reg is an atomic pointer so SIGHUP reload swaps the whole query set
	// without a lock: request goroutines load, reload stores. The old
	// registry is never mutated — in-flight requests keep a consistent view.
	reg    atomic.Pointer[registry.Registry]
	run    Runner
	tokens map[string]string // caller -> token
	aud    *audit.Writer
}

// New assembles the server. It performs no validation of its own: Runner
// hides the Engine behind an interface, so the caller constructing a real
// *exec.Engine (serve, T10) must itself refuse to start when
// Engine.DefaultTimeout <= 0 or Engine.MaxRows <= 0.
//
// tokens must have distinct values — meta.LoadConfig enforces this;
// hand-built maps with duplicates give nondeterministic authz.
func New(reg *registry.Registry, run Runner, tokens map[string]string, aud *audit.Writer) *Server {
	s := &Server{run: run, tokens: tokens, aud: aud}
	s.reg.Store(reg)
	return s
}

// SetRegistry atomically replaces the served query set (SIGHUP hot reload).
// Callers must pass a fully-loaded registry: serve refuses to start and
// reloads abort on LoadDir errors before reaching here.
func (s *Server) SetRegistry(reg *registry.Registry) {
	s.reg.Store(reg)
}

// Handler routes POST /q/{name} (other methods get Go's 405).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /q/{name}", s.handleQuery)
	return mux
}

func (s *Server) handleQuery(w http.ResponseWriter, r *http.Request) {
	// Order per contract: query existence (404), token (401), allow (403).
	name := r.PathValue("name")
	var q *queryfile.Query
	if reg := s.reg.Load(); reg != nil {
		q = reg.Get(name)
	}
	if q == nil {
		problem(w, http.StatusNotFound, "query not found", fmt.Sprintf("no query named %q is loaded", name))
		return
	}
	caller := s.callerFor(r.Header.Get("X-Plinth-Token"))
	if caller == "" {
		problem(w, http.StatusUnauthorized, "unauthorized", "missing or unknown token")
		return
	}
	if !q.Allows(caller) {
		problem(w, http.StatusForbidden, "forbidden", fmt.Sprintf("caller %q is not allowed to run query %q", caller, name))
		return
	}

	var body map[string]any
	if err := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes)).Decode(&body); err != nil {
		problem(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}
	// JSON null normalizes to absent BEFORE validation and Run: absent is
	// the exec contract's spelling of NULL, so nulls are never params.
	args := normalizeNulls(body)

	// Validate against the same contract the engine enforces so bad input
	// is a 400 here rather than a 500 out of the execution layer. The
	// normalized raw map (not the coerced one) goes to Run — the engine
	// stays the single source of truth for coercion, defaults and NULLs.
	if _, err := exec.Coerce(q.Params, args); err != nil {
		problem(w, http.StatusBadRequest, "invalid parameters", err.Error())
		return
	}

	res, err := s.run.Run(r.Context(), q, args)
	if err != nil {
		werr := fmt.Errorf("query %q: %w", name, err)
		s.record(audit.Record{TS: time.Now().UTC(), Caller: caller, Query: name, Params: args, Status: "error", Err: werr.Error()})
		problem(w, http.StatusInternalServerError, "execution failed", werr.Error())
		return
	}
	s.record(audit.Record{TS: time.Now().UTC(), Caller: caller, Query: name, Params: args, Status: "ok", Rows: len(res.Rows), Ms: res.DurationMs})

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Plinth-Rows", strconv.Itoa(len(res.Rows)))
	w.Header().Set("X-Plinth-Duration-Ms", strconv.FormatInt(res.DurationMs, 10))
	w.WriteHeader(http.StatusOK)
	// Rows carry pgx-native values; their MarshalJSON handles the types.
	_ = json.NewEncoder(w).Encode(map[string]any{"rows": res.Rows})
}

// callerFor maps a presented token to its caller name ("" = unauthenticated).
// Exact-match compare; constant-time defense is out of scope for v2.
func (s *Server) callerFor(token string) string {
	if token == "" {
		return ""
	}
	for caller, tok := range s.tokens {
		if tok == token {
			return caller
		}
	}
	return ""
}

// normalizeNulls strips JSON-null values in place and guarantees a non-nil
// map (a literal `null` body decodes to nil).
func normalizeNulls(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	for k, v := range in {
		if v == nil {
			delete(in, k)
		}
	}
	return in
}

// record appends one audit line; failures are logged and the request
// proceeds (availability over audit completeness). A nil writer is tolerated
// so New remains usable without an audit sink.
func (s *Server) record(rec audit.Record) {
	if s.aud == nil {
		return
	}
	if err := s.aud.Record(rec); err != nil {
		log.Printf("audit write failed (continuing): query %q caller %q: %v", rec.Query, rec.Caller, err)
	}
}

// problem writes an RFC 7807 response: {type, title, status, detail}.
func problem(w http.ResponseWriter, status int, title, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type":   "about:blank",
		"title":  title,
		"status": status,
		"detail": detail,
	})
}
