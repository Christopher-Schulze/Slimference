package proxy

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
)

func TestWSSStatefulSafePackageAuditZeroJSONCompactsFullHistoryTurn(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    string
		output  string
	}{
		{
			name:    "npm audit",
			command: "npm audit --json",
			want:    "[npm audit] 0 vulnerabilities",
			output:  wssPackageAuditEnvelope("npm-audit-zero", wssNpmAuditZeroJSONFixture(180)),
		},
		{
			name:    "pnpm audit",
			command: "pnpm audit --json=1",
			want:    "[pnpm audit] 0 vulnerabilities",
			output:  wssPackageAuditEnvelope("pnpm-audit-zero", wssPnpmAuditZeroJSONFixture(180)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Defaults()
			cfg.Compression.OutputReduce.StopSequencesEnabled = false
			cfg.Compression.OutputReduce.BeTerseHintEnabled = false
			cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
			cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
			p := New(cfg)
			adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
			cacheKey := fmt.Sprintf("stateful-package-audit-safe-session-%p", p)

			env := parseWSJSON(t, wssCommandOutputRequestBody("resp-"+tt.name, "call_"+strings.ReplaceAll(tt.name, " ", "_"), tt.command, tt.output, cacheKey))
			replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
			if err != nil {
				t.Fatalf("handle package audit zero JSON request: %v", err)
			}
			if !replace {
				t.Fatal("full-history package audit zero JSON should compact")
			}
			body := string(env.Body)
			if !strings.Contains(body, tt.want) ||
				!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
				strings.Contains(body, "pkg-179") {
				t.Fatalf("package audit zero JSON was not archive-backed compacted: %s", body)
			}
			summary := p.DebugRecorder().Last(1, false)[0]
			if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
				summary.DebugFacts["wss.request_shape"] != "full_history" {
				t.Fatalf("stateful-safe package audit zero JSON should save without structured guard: %+v", summary)
			}
		})
	}
}

func TestWSSStatefulSafePackageAuditRejectsUnsafeOrAmbiguousReports(t *testing.T) {
	tests := []struct {
		name         string
		command      string
		output       string
		mustPreserve string
	}{
		{
			name:         "nonzero vulnerability JSON",
			command:      "npm audit --json",
			output:       wssPackageAuditEnvelope("npm-audit-high", wssNpmAuditNonzeroJSONFixture()),
			mustPreserve: "lodash",
		},
		{
			name:    "text zero report",
			command: "npm audit",
			output: wssPackageAuditEnvelope("npm-audit-text", strings.Repeat("auditing dependency tree\n", 80)+
				"found 0 vulnerabilities\n"),
			mustPreserve: "found 0 vulnerabilities",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Defaults()
			cfg.Compression.OutputReduce.StopSequencesEnabled = false
			cfg.Compression.OutputReduce.BeTerseHintEnabled = false
			cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
			cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
			p := New(cfg)
			adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
			cacheKey := fmt.Sprintf("stateful-package-audit-unsafe-session-%p", p)

			env := parseWSJSON(t, wssCommandOutputRequestBody("resp-"+tt.name, "call_"+strings.ReplaceAll(tt.name, " ", "_"), tt.command, tt.output, cacheKey))
			_, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
			if err != nil {
				t.Fatalf("handle unsafe package audit request: %v", err)
			}
			body := string(env.Body)
			if strings.Contains(body, "[npm audit] 0 vulnerabilities") ||
				strings.Contains(body, "[pnpm audit] 0 vulnerabilities") ||
				!strings.Contains(body, tt.mustPreserve) {
				t.Fatalf("unsafe package audit output hid relevant state: body=%s", body)
			}
		})
	}
}

func TestWSSCompactedPackageSuccessSummaryAcceptsOnlyExactAuditZero(t *testing.T) {
	t.Parallel()
	if !wssCompactedPackageSuccessSummary([]byte(`{"metadata":{"vulnerabilities":{"info":0,"low":0,"moderate":0,"high":0,"critical":0,"total":0}}}`), []byte("[npm audit] 0 vulnerabilities\n")) {
		t.Fatal("exact npm audit zero-vulnerability summary should be accepted")
	}
	rejects := [][]byte{
		[]byte("[npm audit] ok\n"),
		[]byte("[npm audit] found 0 vulnerabilities\n"),
		[]byte("[pnpm audit] 1 vulnerability\n"),
		[]byte("[yarn audit] 0 vulnerabilities\n"),
	}
	for _, compacted := range rejects {
		t.Run(string(compacted), func(t *testing.T) {
			t.Parallel()
			if wssCompactedPackageSuccessSummary(nil, compacted) {
				t.Fatalf("ambiguous audit summary accepted: %q", compacted)
			}
		})
	}
}

func wssPackageAuditEnvelope(id, output string) string {
	return "Chunk ID: " + id + "\n" +
		"Wall time: 0.0010 seconds\n" +
		"Process exited with code 0\n" +
		"Original token count: 10000\n" +
		"Output:\n" +
		output
}

func wssNpmAuditZeroJSONFixture(dependencies int) string {
	var b strings.Builder
	b.WriteString("{\n")
	b.WriteString(`  "auditReportVersion": 2,` + "\n")
	b.WriteString(`  "vulnerabilities": {},` + "\n")
	b.WriteString(`  "metadata": {` + "\n")
	b.WriteString(`    "vulnerabilities": {"info":0,"low":0,"moderate":0,"high":0,"critical":0,"total":0},` + "\n")
	b.WriteString(`    "dependencies": {` + "\n")
	for i := range dependencies {
		comma := ","
		if i == dependencies-1 {
			comma = ""
		}
		fmt.Fprintf(&b, `      "pkg-%03d": {"version":"1.%d.0","dev":%t}%s`+"\n", i, i, i%2 == 0, comma)
	}
	b.WriteString("    }\n")
	b.WriteString("  }\n")
	b.WriteString("}\n")
	return b.String()
}

func wssPnpmAuditZeroJSONFixture(dependencies int) string {
	var b strings.Builder
	b.WriteString("{\n")
	b.WriteString(`  "advisories": {},` + "\n")
	b.WriteString(`  "actions": [],` + "\n")
	b.WriteString(`  "metadata": {` + "\n")
	b.WriteString(`    "vulnerabilities": {"info":0,"low":0,"moderate":0,"high":0,"critical":0,"total":0},` + "\n")
	b.WriteString(`    "dependencies": {` + "\n")
	for i := range dependencies {
		comma := ","
		if i == dependencies-1 {
			comma = ""
		}
		fmt.Fprintf(&b, `      "pkg-%03d": {"version":"2.%d.0","optional":%t}%s`+"\n", i, i, i%3 == 0, comma)
	}
	b.WriteString("    }\n")
	b.WriteString("  }\n")
	b.WriteString("}\n")
	return b.String()
}

func wssNpmAuditNonzeroJSONFixture() string {
	return `{
  "auditReportVersion": 2,
  "vulnerabilities": {
    "lodash": {
      "name": "lodash",
      "severity": "high",
      "via": [{"title":"Prototype Pollution","severity":"high"}],
      "effects": [],
      "range": "<4.17.21",
      "nodes": ["node_modules/lodash"],
      "fixAvailable": true
    }
  },
  "metadata": {
    "vulnerabilities": {"info":0,"low":0,"moderate":0,"high":1,"critical":0,"total":1}
  }
}
`
}
