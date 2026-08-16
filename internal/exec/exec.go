// Package exec rewrites :name parameters to $N and runs queries through a
// read-only pool with timeout and row cap.
package exec

import (
	"context"
	"fmt"
	"time"

	"github.com/dayuer/plinth/internal/queryfile"
	"github.com/dayuer/plinth/internal/sqlscan"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Engine struct {
	Pool           *pgxpool.Pool
	DefaultTimeout time.Duration
	MaxRows        int
}

type Result struct {
	Rows       []map[string]any
	DurationMs int64
}

func (e *Engine) Run(ctx context.Context, q *queryfile.Query, in map[string]any) (*Result, error) {
	args, err := Coerce(q.Params, in)
	if err != nil {
		return nil, err
	}
	sqlText, order, err := sqlscan.Rewrite(q.SQL)
	if err != nil {
		return nil, err
	}
	if err := matchParams(q.Params, order); err != nil {
		return nil, err
	}
	timeout := e.DefaultTimeout
	if q.TimeoutMs > 0 {
		timeout = time.Duration(q.TimeoutMs) * time.Millisecond
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	rows, err := e.Pool.Query(cctx, sqlText, bind(order, args)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	res := &Result{Rows: []map[string]any{}}
	fd := rows.FieldDescriptions()
	for rows.Next() {
		if len(res.Rows) >= e.MaxRows {
			return nil, fmt.Errorf("row limit %d exceeded", e.MaxRows)
		}
		vals, err := rows.Values()
		if err != nil {
			return nil, err
		}
		row := map[string]any{}
		for i := range fd {
			row[string(fd[i].Name)] = vals[i]
		}
		res.Rows = append(res.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	res.DurationMs = time.Since(start).Milliseconds()
	return res, nil
}

// matchParams requires declared params and SQL params to be exactly equal sets.
func matchParams(ps []queryfile.Param, order []string) error {
	declared := map[string]bool{}
	for _, p := range ps {
		declared[p.Name] = true
	}
	seen := map[string]bool{}
	for _, n := range order {
		if !declared[n] {
			return fmt.Errorf("SQL uses undeclared parameter %q", n)
		}
		seen[n] = true
	}
	for _, p := range ps {
		if !seen[p.Name] {
			return fmt.Errorf("declared parameter %q does not appear in SQL", p.Name)
		}
	}
	return nil
}

func bind(order []string, args map[string]any) []any {
	out := make([]any, len(order))
	for i, name := range order {
		out[i] = args[name]
	}
	return out
}
