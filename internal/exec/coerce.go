package exec

import (
	"fmt"
	"math"
	"strconv"

	"github.com/dayuer/plinth/internal/queryfile"
)

// Coerce validates raw input values against the declared params and returns
// bind-ready typed values: int→int64, float→float64, bool→bool, str→string.
// An absent optional param with no default binds nil (SQL NULL); with a
// default, the default string is parsed by the param type using the same
// parsers queryfile validation implies (ParseInt 64 / ParseFloat / ParseBool).
// Unknown input keys are rejected so callers cannot smuggle extra
// parameters past the declared contract.
func Coerce(ps []queryfile.Param, in map[string]any) (map[string]any, error) {
	declared := map[string]bool{}
	for _, p := range ps {
		declared[p.Name] = true
	}
	for k := range in {
		if !declared[k] {
			return nil, fmt.Errorf("unknown parameter %q", k)
		}
	}
	out := make(map[string]any, len(ps))
	for _, p := range ps {
		v, present := in[p.Name]
		if !present {
			switch {
			case p.Required:
				return nil, fmt.Errorf("missing required parameter %q", p.Name)
			case p.Default != "":
				dv, err := parseDefault(p.Type, p.Default)
				if err != nil {
					return nil, fmt.Errorf("parameter %q: bad default %q: %v", p.Name, p.Default, err)
				}
				out[p.Name] = dv
			default:
				out[p.Name] = nil // NULL
			}
			continue
		}
		cv, err := coerceValue(p, v)
		if err != nil {
			return nil, err
		}
		out[p.Name] = cv
	}
	return out, nil
}

func parseDefault(typ, val string) (any, error) {
	switch typ {
	case "int":
		return strconv.ParseInt(val, 10, 64)
	case "float":
		return strconv.ParseFloat(val, 64)
	case "bool":
		return strconv.ParseBool(val)
	default: // str
		return val, nil
	}
}

func coerceValue(p queryfile.Param, v any) (any, error) {
	switch p.Type {
	case "int":
		// JSON numbers arrive as float64; integral ones map to int64.
		// 1<<63 as float64 bounds int64: >= rejects 2^63 up, < -2^63
		// rejects below, so the conversion never overflows.
		if n, ok := v.(float64); ok {
			if math.Trunc(n) != n || n >= float64(1<<63) || n < -float64(1<<63) {
				return nil, fmt.Errorf("parameter %q: want int, got fractional/out-of-range float64 %v", p.Name, n)
			}
			return int64(n), nil
		}
		if n, ok := v.(int64); ok {
			return n, nil
		}
	case "float":
		if f, ok := v.(float64); ok {
			return f, nil
		}
	case "bool":
		if b, ok := v.(bool); ok {
			return b, nil
		}
	case "str":
		if s, ok := v.(string); ok {
			return s, nil
		}
	}
	return nil, fmt.Errorf("parameter %q: want %s, got %T", p.Name, p.Type, v)
}
