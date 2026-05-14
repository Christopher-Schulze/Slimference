package filter

import (
	"strings"
	"testing"
)

func TestTryCompactPsql(t *testing.T) {
	t.Parallel()
	out, ok := TryCompactPsql([]string{"psql", "-c", "select 1"}, []byte(""))
	if !ok || string(out) != "[psql] ok\n" {
		t.Fatalf("ok=%v %q", ok, out)
	}
	mysqlOut, ok := TryCompactPsql([]string{"mysql", "-e", "select 1"}, []byte(""))
	if !ok || string(mysqlOut) != "[mysql] ok\n" {
		t.Fatalf("mysql ok=%v %q", ok, mysqlOut)
	}
	mariadbOut, ok := TryCompactPsql([]string{"mariadb", "-e", "select 1"}, []byte(""))
	if !ok || string(mariadbOut) != "[mariadb] ok\n" {
		t.Fatalf("mariadb ok=%v %q", ok, mariadbOut)
	}
	sqliteOut, ok := TryCompactPsql([]string{"sqlite3", "db.sqlite", "select 1"}, []byte(""))
	if !ok || string(sqliteOut) != "[sqlite] ok\n" {
		t.Fatalf("sqlite ok=%v %q", ok, sqliteOut)
	}
	if _, ok := TryCompactPsql([]string{"redis-cli", "ping"}, []byte("")); ok {
		t.Fatal("not a SQL shell")
	}
}

func TestTryCompactPsql_tableBorders(t *testing.T) {
	t.Parallel()
	// Typical psql table output with ASCII borders.
	input := ` id | name  | email
----+-------+--------------------
  1 | alice | alice@example.com
  2 | bob   | bob@example.com
(2 rows)
`
	out, ok := TryCompactPsql([]string{"psql", "-c", "select * from users"}, []byte(input))
	if !ok {
		t.Fatalf("expected table border compaction: %q", out)
	}
	s := string(out)
	// Separator lines should be gone.
	if strings.Contains(s, "----") {
		t.Errorf("separator lines should be stripped, got %q", s)
	}
	// Data should be present.
	if !strings.Contains(s, "alice") || !strings.Contains(s, "bob") {
		t.Errorf("data rows should be retained, got %q", s)
	}
	if !strings.Contains(s, "(2 rows)") {
		t.Errorf("row count should be retained, got %q", s)
	}
	if len(s) >= len(input) {
		t.Errorf("compact output should be shorter: got %d vs %d", len(s), len(input))
	}
}

func TestTryCompactPsql_mysqlTable(t *testing.T) {
	t.Parallel()
	input := `+----+-------+-------------------+
| id | name  | email             |
+----+-------+-------------------+
|  1 | alice | alice@example.com |
|  2 | bob   | bob@example.com   |
+----+-------+-------------------+
2 rows in set (0.00 sec)
`
	out, ok := TryCompactPsql([]string{"mysql", "-e", "select * from users"}, []byte(input))
	if !ok {
		t.Fatalf("expected mysql table compaction: %q", out)
	}
	s := string(out)
	if strings.Contains(s, "+----") {
		t.Fatalf("mysql borders should be stripped, got %q", s)
	}
	for _, want := range []string{"id | name | email", "alice@example.com", "2 rows in set"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in %q", want, s)
		}
	}
	if len(s) >= len(input) {
		t.Fatalf("compact output should be shorter: got %d vs %d", len(s), len(input))
	}
}

func TestTryCompactPsql_guards(t *testing.T) {
	t.Parallel()
	// len < 1
	if _, ok := TryCompactPsql([]string{}, []byte("")); ok {
		t.Fatal("len<1 should return false")
	}
	// non-empty stdout
	if _, ok := TryCompactPsql([]string{"psql"}, []byte("result\n")); ok {
		t.Fatal("non-empty stdout should return false")
	}
}

// TestCompactPsqlOutput_allSeparators covers the len(out)==0 → "" return branch.
func TestCompactPsqlOutput_allSeparators(t *testing.T) {
	t.Parallel()
	// Only separator lines → out is empty → returns ""
	input := "--------+--------\n--------+--------\n"
	got := compactPsqlOutput(input)
	if got != "" {
		t.Errorf("all-separator input: want empty string, got %q", got)
	}
}

// TestCompactPsqlOutput_nonTableLine covers the plain line (no "|") fallthrough.
func TestCompactPsqlOutput_nonTableLine(t *testing.T) {
	t.Parallel()
	// A line with no "|" and not a separator → appended as-is
	input := "SELECT 1\n(1 row)\n"
	got := compactPsqlOutput(input)
	if !strings.Contains(got, "SELECT 1") {
		t.Errorf("non-table line should be retained, got %q", got)
	}
}

// TestCompactPsqlOutput_borderStyleRow covers the start++ and end-- loops (lines 55-60):
// psql border-style rows like "| col1 | col2 |" produce empty first and last cols after split.
func TestCompactPsqlOutput_borderStyleRow(t *testing.T) {
	t.Parallel()
	// "| col1 | col2 |" splits by "|" into ["", " col1 ", " col2 ", ""].
	// TrimSpace each: ["", "col1", "col2", ""].
	// start++ fires (trimmed[0]=="") and end-- fires (trimmed[3]=="").
	got := compactPsqlOutput("| col1 | col2 |\n| a    | b    |\n")
	if !strings.Contains(got, "col1") || !strings.Contains(got, "col2") {
		t.Errorf("border-style row: want col names, got %q", got)
	}
	if strings.HasPrefix(got, " |") {
		t.Errorf("border-style row: leading empty col should be stripped, got %q", got)
	}
}

func TestSQLShellLabel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		argv []string
		want string
	}{
		{[]string{"psql.exe"}, "psql"},
		{[]string{"mysql.exe"}, "mysql"},
		{[]string{"mariadb.exe"}, "mariadb"},
		{[]string{"sqlite"}, "sqlite"},
		{[]string{"sqlite.exe"}, "sqlite"},
		{[]string{"sqlite3.exe"}, "sqlite"},
		{[]string{"duckdb"}, ""},
		{nil, ""},
	}
	for _, tt := range tests {
		if got := sqlShellLabel(tt.argv); got != tt.want {
			t.Fatalf("sqlShellLabel(%v)=%q want %q", tt.argv, got, tt.want)
		}
	}
}
