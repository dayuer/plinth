package readcheck

import (
	"strings"
	"testing"
)

func TestAcceptsReadonly(t *testing.T) {
	ok := []string{
		"SELECT 1",
		"select * from t where a = 1",
		"WITH x AS (SELECT 1) SELECT * FROM x",
		"SELECT count(*) FROM invoices GROUP BY org_id",
		"SELECT 'insert into is just data' FROM t",
		"SELECT * FROM t -- comment says update",
		"SELECT * FROM t WHERE note = $$delete me$$",
		// Word-boundary false-positive guards: each contains a banned
		// word as a substring but must pass because the scan works on
		// whole words.
		"SELECT updated_at, setting, deleted FROM t",
		"SELECT lock_owner FROM t",
		// (A column literally named pg_sleepers is deliberately absent:
		// the PG_SLEEP* prefix ban over-rejects it — the safe direction.)
	}
	for _, sql := range ok {
		if err := Check(sql); err != nil {
			t.Errorf("Check(%q) = %v, want nil", sql, err)
		}
	}
}

func TestRejectsWritesAndDanger(t *testing.T) {
	// wantSubstring pins WHY each statement is rejected, so a different
	// (or accidental) rule firing fails the test.
	bad := []struct {
		sql           string
		wantSubstring string
	}{
		{"SELECT 1; DROP TABLE t", "statement separator"},
		{"WITH d AS (DELETE FROM t RETURNING *) SELECT * FROM d", "DELETE"},
		{"SELECT * INTO newtable FROM t", "INTO"},
		{"SELECT * FROM t FOR UPDATE", "UPDATE"},
		// Leading verbs: the first-word rule echoes the verb it saw.
		{"INSERT INTO t VALUES (1)", "INSERT"},
		{"UPDATE t SET a = 1", "UPDATE"},
		{"DELETE FROM t", "DELETE"},
		{"CREATE TABLE x (id int)", "CREATE"},
		{"ALTER TABLE t ADD COLUMN x int", "ALTER"},
		{"DROP TABLE t", "DROP"},
		{"TRUNCATE t", "TRUNCATE"},
		{"GRANT SELECT ON t TO someone", "GRANT"},
		{"COPY t FROM '/etc/passwd'", "COPY"},
		{"CALL some_proc()", "CALL"},
		// Dangerous functions reachable from a SELECT with no privileges
		// beyond a connection.
		{"SELECT pg_sleep(10)", "PG_SLEEP"},
		{"SELECT pg_sleep_for(interval '1h')", "PG_SLEEP"},
		{"SELECT pg_read_file('postgresql.conf')", "PG_READ_FILE"},
		{"SELECT lo_import('/etc/passwd')", "LO_IMPORT"},
		{"SELECT set_config('search_path','x',false)", "SET_CONFIG"},
		{"SELECT pg_advisory_lock(42)", "PG_ADVISORY_"},
		{"SELECT pg_notify('c','p')", "PG_NOTIFY"},
		{"SELECT * FROM dblink('h','SELECT 1') AS t(x int)", "DBLINK"},
		{"SELECT nextval('s')", "NEXTVAL"},
		{"SELECT setval('s',1)", "SETVAL"},
		{"SELECT lo_get(1)", "LO_GET"},
		{"SELECT loread(0, 100)", "LOREAD"},
		{"SELECT lo_open(1, 131072)", "LO_OPEN"},
		{"SELECT lo_close(1)", "LO_CLOSE"},
		{"SELECT lowrite(fd, data)", "LOWRITE"},
		{"SELECT lo_unlink(1)", "LO_UNLINK"},
		{"SELECT pg_stat_reset()", "PG_STAT_RESET"},
		{"SELECT pg_reload_conf()", "PG_RELOAD_CONF"},
		{"SELECT pg_try_advisory_lock(1)", "PG_TRY_ADVISORY_"},
		{"SELECT pg_try_advisory_xact_lock_shared(1)", "PG_TRY_ADVISORY_"},
		{"SELECT pg_stat_reset_slru('x')", "PG_STAT_RESET"},
		{"SELECT lo_create(0)", "LO_CREATE"},
		{"SELECT lo_creat(131072)", "LO_CREAT"},
		{"SELECT pg_ls_waldir()", "PG_LS_"},
		{"SELECT 1; ", "statement separator"},
		// Identifier-adjacent '$$' is code, not a dollar quote (PostgreSQL
		// lexes '$' as identifier-continue), so the ';' and verbs stay
		// visible in the blanked SQL and must still be caught.
		{"SELECT a$$b; DROP TABLE t; SELECT x$$y, $$ $$, $$ $$", "statement separator"},
		{"SELECT é$$b; DELETE FROM t; SELECT c$$d", "statement separator"},
	}
	for _, tc := range bad {
		err := Check(tc.sql)
		if err == nil {
			t.Errorf("Check(%q) = nil, want error containing %q", tc.sql, tc.wantSubstring)
			continue
		}
		if !strings.Contains(err.Error(), tc.wantSubstring) {
			t.Errorf("Check(%q) = %q, want error containing %q", tc.sql, err, tc.wantSubstring)
		}
	}
}

func TestRejectsBrokenLexing(t *testing.T) {
	for _, sql := range []string{"SELECT 'unterminated", "SELECT E'x\\'"} {
		if err := Check(sql); err == nil {
			t.Errorf("Check(%q) = nil, want error", sql)
		}
	}
}

func TestRejectsPositionalParams(t *testing.T) {
	// Raw positional parameters would alias with the $N placeholders the
	// rewriter emits for :name parameters, so they are banned outright.
	for _, sql := range []string{"SELECT $1, :name", "WHERE x = $2"} {
		if err := Check(sql); err == nil {
			t.Errorf("Check(%q) = nil, want error", sql)
		}
	}
	// '$' without a following digit is just an identifier character.
	if err := Check("SELECT a$b"); err != nil {
		t.Errorf("Check(%q) = %v, want nil", "SELECT a$b", err)
	}
}
