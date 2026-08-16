// Package sqlscan walks SQL with full awareness of comments, string
// literals and casts. It powers read-only checking (Analyze) and named
// parameter rewriting (Rewrite).
package sqlscan

import (
	"fmt"
	"strings"
)

func isIdentChar(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// walk traverses sql.
//
// emit receives every non-parameter chunk verbatim (comments and literals
// included). Contract: comments/literals are emitted as single
// multi-character chunks; verbatim emissions are exactly 1 byte — Analyze
// relies on this to decide blanking. param receives each :name occurrence
// and returns the text to substitute.
func walk(sql string, emit func(string), param func(name string) string) error {
	n := len(sql)
	i := 0
	for i < n {
		c := sql[i]
		switch {
		case c == '-' && i+1 < n && sql[i+1] == '-':
			j := i
			for j < n && sql[j] != '\n' {
				j++
			}
			emit(sql[i:j])
			i = j
		case c == '/' && i+1 < n && sql[i+1] == '*':
			j := i + 2
			for j < n && !(sql[j] == '*' && j+1 < n && sql[j+1] == '/') {
				j++
			}
			if j >= n {
				return fmt.Errorf("unterminated block comment")
			}
			emit(sql[i : j+2])
			i = j + 2
		case c == '\'':
			if i > 0 && (sql[i-1] == 'e' || sql[i-1] == 'E') && (i < 2 || !isIdentChar(sql[i-2])) {
				return fmt.Errorf("E'' string literals are not supported")
			}
			j := i + 1
			for j < n {
				if sql[j] == '\\' {
					return fmt.Errorf("backslash inside string literal is not supported")
				}
				if sql[j] == '\'' {
					if j+1 < n && sql[j+1] == '\'' {
						j += 2
						continue
					}
					break
				}
				j++
			}
			if j >= n {
				return fmt.Errorf("unterminated string literal")
			}
			emit(sql[i : j+1])
			i = j + 1
		case c == '$' && i+1 < n && sql[i+1] == '$' &&
			(i == 0 || !(isIdentChar(sql[i-1]) || sql[i-1] == '$' || sql[i-1] >= 0x80)):
			// PostgreSQL lexes '$' as an identifier-continue byte (and
			// non-ASCII bytes as identifier bytes): a$$b is ONE identifier,
			// not ident + dollar-quote. So '$$' opens a dollar quote only
			// when the preceding byte cannot be part of an identifier;
			// otherwise these '$' bytes are ordinary code — fall to default,
			// which emits one '$' per iteration, so the next '$' of a $$$
			// run is re-examined (and again rejected: its predecessor is
			// '$').
			j := i + 2
			for j < n && !(sql[j] == '$' && j+1 < n && sql[j+1] == '$') {
				j++
			}
			if j >= n {
				return fmt.Errorf("unterminated dollar-quoted string")
			}
			emit(sql[i : j+2])
			i = j + 2
		case c == ':' && i+1 < n && sql[i+1] == ':':
			// cast operator (::) — consume both colons so the second one
			// is not mistaken for a parameter introducer (e.g. ::text).
			emit(":")
			emit(":")
			i += 2
		case c == ':':
			j := i + 1
			for j < n && isIdentChar(sql[j]) {
				j++
			}
			if j == i+1 { // lone ':'
				emit(":")
				i++
				continue
			}
			emit(param(sql[i+1 : j]))
			i = j
		default:
			emit(sql[i : i+1])
			i++
		}
	}
	return nil
}

// Analyze returns SQL safe for keyword scanning: every comment and string
// literal chunk has its contents blanked (newlines preserved), and :name
// parameters are blanked. Also returns distinct parameters in
// first-appearance order. On error, the other return values are partial
// and must be discarded.
func Analyze(sql string) (clean string, params []string, err error) {
	seen := map[string]bool{}
	var b strings.Builder
	blank := func(s string) string {
		var sb strings.Builder
		for i := 0; i < len(s); i++ {
			if s[i] == '\n' {
				sb.WriteByte('\n')
			} else {
				sb.WriteByte(' ')
			}
		}
		return sb.String()
	}
	err = walk(sql,
		func(chunk string) {
			if len(chunk) == 1 {
				b.WriteByte(chunk[0])
				return
			}
			b.WriteString(blank(chunk))
		},
		func(name string) string {
			if !seen[name] {
				seen[name] = true
				params = append(params, name)
			}
			return ":" + strings.Repeat(" ", len(name))
		},
	)
	return b.String(), params, err
}

// Rewrite converts :name placeholders to $N in first-appearance order and
// returns the ordered parameter names. Comments and literals pass through
// untouched. On error, the other return values are partial and must be
// discarded.
func Rewrite(sql string) (string, []string, error) {
	idx := map[string]int{}
	var order []string
	var b strings.Builder
	err := walk(sql,
		func(chunk string) { b.WriteString(chunk) },
		func(name string) string {
			if n, ok := idx[name]; ok {
				return fmt.Sprintf("$%d", n)
			}
			idx[name] = len(order) + 1
			order = append(order, name)
			return fmt.Sprintf("$%d", idx[name])
		},
	)
	return b.String(), order, err
}
