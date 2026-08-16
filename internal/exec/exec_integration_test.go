//go:build integration

package exec

import (
	"strings"
	"testing"
	"time"

	"github.com/dayuer/plinth/internal/queryfile"
	"github.com/dayuer/plinth/test/integration"
)

func newEngine(t *testing.T) *Engine {
	t.Helper()
	pool := integration.StartPG(t)
	integration.ApplyFixtureOn(t, pool)
	return &Engine{Pool: pool, DefaultTimeout: 5 * time.Second, MaxRows: 100}
}

const invoiceListSQL = `SELECT id, org_id, status, amount_total, currency
FROM invoices
WHERE org_id = :org_id
  AND (:status::text IS NULL OR status = :status)
ORDER BY id DESC
LIMIT :limit`

func TestRunNamedParams(t *testing.T) {
	eng := newEngine(t)
	q := &queryfile.Query{Name: "t", Mode: "read", SQL: invoiceListSQL,
		Params: []queryfile.Param{
			{Name: "org_id", Type: "int", Required: true},
			{Name: "status", Type: "str"},
			{Name: "limit", Type: "int", Default: "10"},
		}}
	res, err := eng.Run(t.Context(), q, map[string]any{"org_id": float64(1)})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 2 {
		t.Fatalf("rows = %d, want 2: %+v", len(res.Rows), res.Rows)
	}
	if res.Rows[0]["currency"] != "IDR" {
		t.Errorf("row0 = %+v", res.Rows[0])
	}
	res, err = eng.Run(t.Context(), q, map[string]any{"org_id": float64(1), "status": "PAID"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 1 || res.Rows[0]["status"] != "PAID" {
		t.Errorf("filtered rows = %+v", res.Rows)
	}
}

func TestRunParamMismatch(t *testing.T) {
	eng := newEngine(t)
	q := &queryfile.Query{Name: "t", Mode: "read", SQL: "SELECT id FROM invoices WHERE org_id = :org_id",
		Params: []queryfile.Param{{Name: "org_id", Type: "int", Required: true}, {Name: "ghost", Type: "str"}}}
	_, err := eng.Run(t.Context(), q, map[string]any{"org_id": 1.0})
	if err == nil {
		t.Fatal("declared-but-unused param should error")
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunRowCap(t *testing.T) {
	eng := newEngine(t)
	eng.MaxRows = 2
	q := &queryfile.Query{Name: "t", Mode: "read", SQL: "SELECT id FROM invoices", Params: []queryfile.Param{}}
	if _, err := eng.Run(t.Context(), q, nil); err == nil {
		t.Fatal("row cap should error when exceeded")
	}
}

func TestRunTimeout(t *testing.T) {
	eng := newEngine(t)
	q := &queryfile.Query{Name: "t", Mode: "read", SQL: "SELECT pg_sleep(2)", TimeoutMs: 200}
	start := time.Now()
	_, err := eng.Run(t.Context(), q, nil)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("query exceeding TimeoutMs should error")
	}
	if elapsed > 1500*time.Millisecond {
		t.Fatalf("cancellation took %v; context deadline not enforced", elapsed)
	}
}
