// Package queryfile parses queries/*.sql: a leading "-- key: value"
// comment header declaring metadata, then a :name-parameterized SQL body.
package queryfile

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

type Param struct {
	Name     string
	Type     string // int | str | bool | float
	Required bool
	Default  string // raw text, "" = none
}

type Query struct {
	Name        string
	Desc        string
	Params      []Param
	AllowTokens []string
	SemDataset  string
	SemSnapshot string
	Mode        string // always "read" in v2
	TimeoutMs   int    // 0 = engine default
	SQL         string
}

// Allows reports whether caller may invoke this query.
func (q *Query) Allows(caller string) bool {
	for _, c := range q.AllowTokens {
		if c == caller {
			return true
		}
	}
	return false
}

// Parse parses one query file. filename is used to enforce name == file base.
func Parse(filename string, src string) (*Query, error) {
	lines := strings.Split(src, "\n")
	header := map[string]string{}
	bodyStart := len(lines)
	for i, ln := range lines {
		t := strings.TrimSpace(ln)
		if !strings.HasPrefix(t, "-- ") {
			bodyStart = i
			break
		}
		rest := strings.TrimSpace(t[3:])
		// Spec §3 writes the name line as "-- plinth: name: X": the
		// "plinth:" marker namespaces the header, other keys are bare.
		// Strip it so the line keys as "name", not "plinth".
		if strings.HasPrefix(rest, "plinth:") {
			rest = strings.TrimSpace(rest[len("plinth:"):])
		}
		colon := strings.Index(rest, ":")
		if colon <= 0 {
			return nil, fmt.Errorf("%s: malformed header line %d: %q", filename, i+1, ln)
		}
		k := strings.TrimSpace(rest[:colon])
		if _, dup := header[k]; dup {
			return nil, fmt.Errorf("%s: duplicate header key %q", filename, k)
		}
		header[k] = strings.TrimSpace(rest[colon+1:])
	}
	if len(header) == 0 {
		return nil, fmt.Errorf("%s: missing header; start the file with '-- plinth: name: ...' lines", filename)
	}

	q := &Query{Mode: "read"}
	q.Name = header["name"]
	q.Desc = header["desc"]
	if q.Name == "" {
		return nil, fmt.Errorf("%s: missing 'name' header", filename)
	}
	base := strings.TrimSuffix(filepath.Base(filename), ".sql")
	if q.Name != base {
		return nil, fmt.Errorf("%s: name %q must equal file base %q", filename, q.Name, base)
	}
	if header["allow-tokens"] == "" {
		return nil, fmt.Errorf("%s: missing 'allow-tokens' header", filename)
	}
	for _, tok := range strings.Split(header["allow-tokens"], ",") {
		if tok = strings.TrimSpace(tok); tok != "" {
			q.AllowTokens = append(q.AllowTokens, tok)
		}
	}
	if header["mode"] != "" {
		q.Mode = header["mode"]
	}
	if q.Mode != "read" {
		return nil, fmt.Errorf("%s: mode %q is reserved; only read queries load in v2", filename, q.Mode)
	}
	if header["semantics"] != "" {
		ds, snap, err := parseSemantics(header["semantics"])
		if err != nil {
			return nil, fmt.Errorf("%s: %v", filename, err)
		}
		q.SemDataset, q.SemSnapshot = ds, snap
	}
	if header["timeout-ms"] != "" {
		n, err := strconv.Atoi(header["timeout-ms"])
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("%s: timeout-ms must be a positive integer", filename)
		}
		q.TimeoutMs = n
	}
	if header["params"] != "" {
		seen := map[string]bool{}
		for _, part := range strings.Split(header["params"], "|") {
			p, err := parseParam(part)
			if err != nil {
				return nil, fmt.Errorf("%s: %v", filename, err)
			}
			if seen[p.Name] {
				return nil, fmt.Errorf("%s: duplicate param %q", filename, p.Name)
			}
			seen[p.Name] = true
			q.Params = append(q.Params, p)
		}
	}
	q.SQL = strings.TrimSpace(strings.Join(lines[bodyStart:], "\n"))
	if q.SQL == "" {
		return nil, fmt.Errorf("%s: empty SQL body", filename)
	}
	return q, nil
}

func parseParam(part string) (Param, error) {
	f := strings.Split(strings.TrimSpace(part), ":")
	if len(f) != 3 {
		return Param{}, fmt.Errorf("param %q: want name:type:required|optional|default", part)
	}
	p := Param{Name: strings.TrimSpace(f[0]), Type: strings.TrimSpace(f[1]), Default: strings.TrimSpace(f[2])}
	if !isIdent(p.Name) || p.Name == "" {
		return Param{}, fmt.Errorf("param %q: invalid name", part)
	}
	switch p.Type {
	case "int", "str", "bool", "float":
	default:
		return Param{}, fmt.Errorf("param %q: type must be int|str|bool|float", part)
	}
	switch {
	case p.Default == "required":
		p.Required = true
		p.Default = ""
	case p.Default == "optional":
		p.Default = ""
	default:
		if err := checkDefault(p.Type, p.Default); err != nil {
			return Param{}, fmt.Errorf("param %q: %v", part, err)
		}
	}
	return p, nil
}

func checkDefault(typ, val string) error {
	switch typ {
	case "int":
		_, err := strconv.Atoi(val)
		return err
	case "float":
		_, err := strconv.ParseFloat(val, 64)
		return err
	case "bool":
		_, err := strconv.ParseBool(val)
		return err
	}
	return nil // str: anything
}

func parseSemantics(v string) (dataset, snapshot string, err error) {
	for _, kv := range strings.Fields(v) {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 {
			return "", "", fmt.Errorf("semantics %q: want 'dataset=X snapshot=Y'", v)
		}
		switch parts[0] {
		case "dataset":
			dataset = parts[1]
		case "snapshot":
			snapshot = parts[1]
		default:
			return "", "", fmt.Errorf("semantics %q: unknown key %q", v, parts[0])
		}
	}
	if dataset == "" || snapshot == "" {
		return "", "", fmt.Errorf("semantics %q: both dataset and snapshot required", v)
	}
	return dataset, snapshot, nil
}

func isIdent(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (i > 0 && c >= '0' && c <= '9') {
			continue
		}
		return false
	}
	return len(s) > 0
}
