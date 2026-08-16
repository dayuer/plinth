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
// table. The pg_* / lo_* and sequence/notify entries cover functions
// reachable from a SELECT that need no privileges beyond a connection, so
// the gate holds even without the read-only role.
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
	"SET_CONFIG": true, "PG_NOTIFY": true, "NEXTVAL": true,
	"SETVAL": true, "PG_STAT_RESET": true, "PG_RELOAD_CONF": true,
	"LO_GET": true, "LOREAD": true, "LO_OPEN": true, "LO_CLOSE": true,
	"LO_UNLINK": true, "LOWRITE": true,
}

// forbiddenPrefixes bans whole function families with one entry. A hit
// over-rejects any identifier sharing the prefix (a column literally
// named pg_sleepers), which is the safe direction: no legitimate
// read-only query needs these families.
var forbiddenPrefixes = []string{
	"PG_SLEEP",     // pg_sleep, pg_sleep_for, pg_sleep_until
	"PG_ADVISORY_", // every pg_advisory_* lock function
	"DBLINK",       // dblink, dblink_exec, dblink_get_*
	"PG_LS_",       // pg_ls_dir/waldir/logdir/tmpdir
}

// Check reports whether sql is a single read-only statement. It must lex
// cleanly under sqlscan (fail-closed: lexing errors reject the SQL), start
// with SELECT or WITH, contain no ';', and contain no forbidden word, no
// forbidden function-family prefix, and no positional parameter outside
// comments and literals. Positional parameters are banned because the
// rewriter turns :name parameters into $N — a raw $1 would alias with
// those.
func Check(sql string) error {
	clean, _, err := sqlscan.Analyze(sql)
	if err != nil {
		return fmt.Errorf("readcheck: cannot analyze SQL: %w", err)
	}
	// PostgreSQL case-folds ASCII only, while Go's strings.ToUpper folds
	// more (e.g. ſ→S); every mismatch direction is therefore
	// gate-stricter or a PostgreSQL syntax error — never under-reject.
	// Do not "fix" this.
	words := words(strings.ToUpper(clean))
	first := ""
	if len(words) > 0 {
		first = words[0]
	}
	if first != "SELECT" && first != "WITH" {
		return fmt.Errorf("readcheck: query must start with SELECT or WITH, got %q", first)
	}
	if strings.Contains(clean, ";") {
		return fmt.Errorf("readcheck: statement separator ';' is not allowed (not even a trailing semicolon)")
	}
	for _, w := range words {
		if forbidden[w] {
			return fmt.Errorf("readcheck: forbidden keyword %s", w)
		}
		for _, p := range forbiddenPrefixes {
			if strings.HasPrefix(w, p) {
				return fmt.Errorf("readcheck: forbidden function family %s*", p)
			}
		}
		if len(w) >= 2 && w[0] == '$' && w[1] >= '0' && w[1] <= '9' {
			return fmt.Errorf("readcheck: positional parameters are not allowed; use :name parameters")
		}
	}
	return nil
}

// words splits s into maximal runs of identifier bytes plus '$'. '$' rides
// along inside words so that a positional parameter ($1) surfaces as a
// word starting with '$', while '$' inside an identifier (a$b) does not —
// matching PostgreSQL, which lexes '$' as an identifier-continue byte.
func words(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		word := r == '$' || r == '_' ||
			(r >= '0' && r <= '9') ||
			(r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z')
		return !word
	})
}
