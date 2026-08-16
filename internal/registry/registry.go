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

// NewForTest builds a registry from explicit queries (test helper).
func NewForTest(qs ...*queryfile.Query) *Registry {
	r := &Registry{queries: map[string]*queryfile.Query{}}
	for _, q := range qs {
		r.queries[q.Name] = q
	}
	return r
}

// add registers q, rejecting a name that is already taken. source is the
// file the query came from, so the error carries the filename.
func (r *Registry) add(q *queryfile.Query, source string) error {
	if _, dup := r.queries[q.Name]; dup {
		return fmt.Errorf("%s: duplicate query name %q", source, q.Name)
	}
	r.queries[q.Name] = q
	return nil
}

// LoadDir reads DIR/queries/*.sql (non-recursive). It returns all problems
// found (never stops at the first) so validate prints a full report.
// A missing queries/ dir is itself one error.
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
		if err := reg.add(q, e.Name()); err != nil {
			errs = append(errs, err)
			continue
		}
	}
	return reg, errs
}
