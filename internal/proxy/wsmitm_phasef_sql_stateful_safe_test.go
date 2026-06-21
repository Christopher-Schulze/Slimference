package proxy

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
)

func TestWSSSafeSQLTableBoundary(t *testing.T) {
	t.Parallel()

	psqlTable := wssSQLTableFixture(8)
	if !wssSafeStatefulStatusCommandOutput("psql -c 'select * from users'", psqlTable) {
		t.Fatal("lossless psql table border compaction should be stateful-safe")
	}
	if !wssSafeStatefulStatusCommandOutput("mysql -e 'select * from users'", wssMySQLTableFixture(8)) {
		t.Fatal("lossless mysql table border compaction should be stateful-safe")
	}
	if wssSafeStatefulStatusCommandOutput("psql -c 'select * from users'", "ERROR: relation \"users\" does not exist\n") {
		t.Fatal("SQL error text must not enter the SQL table stateful-safe gate")
	}
	if wssSafeStatefulStatusCommandOutput("redis-cli ping", psqlTable) {
		t.Fatal("non-SQL shell command must not enter the SQL table stateful-safe gate")
	}
	if wssSafeStatefulStatusCommandOutput("psql -c 'select * from source'", "package main\nfunc main() {}\n") {
		t.Fatal("source-like SQL output must stay guarded")
	}
}

func TestWSSStatefulSafeSQLTableCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	envelope := "Chunk ID: sql-table-safe\nWall time: 0.0010 seconds\nProcess exited with code 0\nOriginal token count: 10000\nOutput:\n" +
		wssSQLTableFixture(80)

	env := parseWSJSON(t, wssCommandOutputRequestBody("resp-sql-table", "call_sql_table", "psql -c 'select * from users'", envelope, "stateful-sql-table-session"))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle SQL table request: %v", err)
	}
	if !replace {
		t.Fatal("full-history lossless SQL table output should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, "user_079@example.com") ||
		!strings.Contains(body, "(80 rows)") ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(body, "----+----------------+----------------------") {
		t.Fatalf("SQL table output was not archive-backed compacted without data loss: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("stateful-safe SQL table should save without structured guard: %+v", summary)
	}
}

func wssSQLTableFixture(rows int) string {
	var b strings.Builder
	b.WriteString(" id  | name           | email\n")
	b.WriteString("-----+----------------+----------------------\n")
	for i := range rows {
		fmt.Fprintf(&b, " %03d | user_%03d       | user_%03d@example.com\n", i, i, i)
	}
	fmt.Fprintf(&b, "(%d rows)\n", rows)
	return b.String()
}

func wssMySQLTableFixture(rows int) string {
	var b strings.Builder
	b.WriteString("+-----+----------------+----------------------+\n")
	b.WriteString("| id  | name           | email                |\n")
	b.WriteString("+-----+----------------+----------------------+\n")
	for i := range rows {
		fmt.Fprintf(&b, "| %03d | user_%03d       | user_%03d@example.com |\n", i, i, i)
	}
	b.WriteString("+-----+----------------+----------------------+\n")
	fmt.Fprintf(&b, "%d rows in set (0.00 sec)\n", rows)
	return b.String()
}
