// Package audit appends execution records to a JSONL file. Masked
// parameters are recorded as "***". Record is safe for concurrent use
// (mutex-serialized writes); write errors are returned to the caller —
// the server logs and continues (availability over audit completeness).
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
	Status string         `json:"status"` // ok | error | denied | bad-request
	Err    string         `json:"err,omitempty"`
}

type Writer struct {
	mu   sync.Mutex
	f    *os.File
	mask map[string]bool
}

// Open creates (appending) the audit file, making parent dirs as needed.
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

// Record masks params (a COPY — the input map is never mutated), marshals,
// and appends one line. Marshal failures of param values are swallowed by
// replacing the whole map with {"audit_marshal_error": true}.
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
		r.Params = map[string]any{"audit_marshal_error": true}
		b, err = json.Marshal(r)
		if err != nil {
			return err
		}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	_, err = w.f.Write(append(b, '\n'))
	return err
}

func (w *Writer) Close() error { return w.f.Close() }
