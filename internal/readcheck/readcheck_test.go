package readcheck

import "testing"

func TestAcceptsReadonly(t *testing.T) {
	ok := []string{
		"SELECT 1",
		"select * from t where a = 1",
		"WITH x AS (SELECT 1) SELECT * FROM x",
		"SELECT count(*) FROM invoices GROUP BY org_id",
		"SELECT 'insert into is just data' FROM t",
		"SELECT * FROM t -- comment says update",
		"SELECT * FROM t WHERE note = $$delete me$$",
	}
	for _, sql := range ok {
		if err := Check(sql); err != nil {
			t.Errorf("Check(%q) = %v, want nil", sql, err)
		}
	}
}

func TestRejectsWritesAndDanger(t *testing.T) {
	bad := []string{
		"SELECT 1; DROP TABLE t",
		"WITH d AS (DELETE FROM t RETURNING *) SELECT * FROM d",
		"SELECT * INTO newtable FROM t",
		"INSERT INTO t VALUES (1)",
		"UPDATE t SET a = 1",
		"DELETE FROM t",
		"CREATE TABLE x (id int)",
		"ALTER TABLE t ADD COLUMN x int",
		"DROP TABLE t",
		"TRUNCATE t",
		"GRANT SELECT ON t TO someone",
		"COPY t FROM '/etc/passwd'",
		"CALL some_proc()",
		"SELECT pg_sleep(10)",
		"SELECT pg_read_file('postgresql.conf')",
		"SELECT lo_import('/etc/passwd')",
		"SELECT 1; ",
		// Identifier-adjacent '$$' is code, not a dollar quote (PostgreSQL
		// lexes '$' as identifier-continue), so the ';' and verbs stay
		// visible in the blanked SQL and must still be caught.
		"SELECT a$$b; DROP TABLE t; SELECT x$$y, $$ $$, $$ $$",
		"SELECT é$$b; DELETE FROM t; SELECT c$$d",
	}
	for _, sql := range bad {
		if err := Check(sql); err == nil {
			t.Errorf("Check(%q) = nil, want error", sql)
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
