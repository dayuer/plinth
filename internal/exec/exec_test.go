package exec

import (
	"testing"

	"github.com/dayuer/plinth/internal/queryfile"
)

func TestCoerce(t *testing.T) {
	params := []queryfile.Param{
		{Name: "org", Type: "int", Required: true},
		{Name: "status", Type: "str"},
		{Name: "limit", Type: "int", Default: "50"},
		{Name: "flag", Type: "bool"},
		{Name: "ratio", Type: "float"},
	}
	got, err := Coerce(params, map[string]any{"org": float64(42), "status": "OPEN", "flag": true, "ratio": 0.5})
	if err != nil {
		t.Fatal(err)
	}
	if got["org"] != int64(42) || got["status"] != "OPEN" || got["flag"] != true || got["ratio"] != 0.5 {
		t.Errorf("got = %v", got)
	}
	if got["limit"] != int64(50) {
		t.Errorf("default not applied: %v", got["limit"])
	}
}

func TestCoerceOptionalNullAndErrors(t *testing.T) {
	got, err := Coerce([]queryfile.Param{{Name: "s", Type: "str"}}, nil)
	if err != nil || got["s"] != nil {
		t.Errorf("optional no default should bind NULL: %v %v", got, err)
	}
	if _, err := Coerce([]queryfile.Param{{Name: "o", Type: "int", Required: true}}, nil); err == nil {
		t.Error("missing required should error")
	}
	if _, err := Coerce([]queryfile.Param{{Name: "o", Type: "int", Required: true}}, map[string]any{"o": "x"}); err == nil {
		t.Error("str for int should error")
	}
	if _, err := Coerce([]queryfile.Param{{Name: "o", Type: "int", Required: true}}, map[string]any{"o": 1.5}); err == nil {
		t.Error("fractional for int should error")
	}
	if _, err := Coerce([]queryfile.Param{{Name: "o", Type: "int", Required: true}}, map[string]any{"o": 1, "ghost": 2}); err == nil {
		t.Error("unknown param should error")
	}
}
