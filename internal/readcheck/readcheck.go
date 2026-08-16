// Package readcheck gates SQL to read-only statements. It is load-time
// gate 1 of the read-only double guarantee: Check scans the SQL that
// sqlscan.Analyze produces with comments and string literals blanked, so
// keywords hidden in data can never match while keywords in code always
// do. The database's read-only role is gate 2.
package readcheck

import (
	"fmt"
	"strings"

	"github.com/dayuer/plinth/internal/sqlscan"
)

// forbidden holds every word that signals a write, schema change, or
// out-of-database reach. INTO is here because SELECT ... INTO creates a
// table. The pg_* / lo_* entries cover dangerous functions reachable
// from a SELECT.
var forbidden = map[string]bool{
	"INSERT": true, "UPDATE": true, "DELETE": true, "MERGE": true,
	"CREATE": true, "ALTER": true, "DROP": true, "TRUNCATE": true,
	"GRANT": true, "REVOKE": true, "COPY": true, "CALL": true,
	"DO": true, "SET": true, "RESET": true, "VACUUM": true,
	"REINDEX": true, "INTO": true, "LOCK": true, "LISTEN": true,
	"NOTIFY": true, "PREPARE": true, "EXECUTE": true, "DISCARD": true,
	"IMPORT": true, "LOAD": true, "CHECKPOINT": true, "REASSIGN": true,
	"PG_READ_FILE": true, "PG_READ_BINARY_FILE": true, "PG_LS_DIR": true,
	"PG_STAT_FILE": true, "PG_SLEEP": true, "PG_TERMINATE_BACKEND": true,
	"PG_CANCEL_BACKEND": true, "LO_IMPORT": true, "LO_EXPORT": true,
}

// Check reports whether sql is a single read-only statement. It must lex
// cleanly under sqlscan (fail-closed: lexing errors reject the SQL), start
// with SELECT or WITH, contain no ';', and contain no forbidden word and
// no positional parameter outside comments and literals. Positional
// parameters are banned because the rewriter turns :name parameters into
// $N — a raw $1 would alias with those.
func Check(sql string) error {
	clean, _, err := sqlscan.Analyze(sql)
	if err != nil {
		return fmt.Errorf("readcheck: cannot analyze SQL: %w", err)
	}
	words := Words(strings.ToUpper(clean))
	if len(words) == 0 || (words[0] != "SELECT" && words[0] != "WITH") {
		return fmt.Errorf("readcheck: statement must start with SELECT or WITH")
	}
	if strings.Contains(clean, ";") {
		return fmt.Errorf("readcheck: multiple statements are not allowed")
	}
	for _, w := range words {
		if forbidden[w] {
			return fmt.Errorf("readcheck: forbidden keyword %s", w)
		}
		if len(w) >= 2 && w[0] == '$' && w[1] >= '0' && w[1] <= '9' {
			return fmt.Errorf("readcheck: positional parameters are not allowed")
		}
	}
	return nil
}

// Words splits s into maximal runs of identifier bytes plus '$'. '$' rides
// along inside words so that a positional parameter ($1) surfaces as a
// word starting with '$', while '$' inside an identifier (a$b) does not —
// matching PostgreSQL, which lexes '$' as an identifier-continue byte.
func Words(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		word := r == '$' || r == '_' ||
			(r >= '0' && r <= '9') ||
			(r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z')
		return !word
	})
}
