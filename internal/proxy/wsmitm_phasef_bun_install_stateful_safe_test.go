package proxy

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
)

func TestWSSStatefulSafeBunInstallCleanSuccessCompactsFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	envelope := "Chunk ID: bun-install-safe\nWall time: 0.0010 seconds\nProcess exited with code 0\nOriginal token count: 10000\nOutput:\n" +
		wssBunInstallCleanFixture(160, true)

	env := parseWSJSON(t, wssCommandOutputRequestBody("resp-bun-install-clean", "call_bun_install_clean", "bun install --ignore-scripts", envelope, "stateful-bun-install-safe-session"))
	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle bun install clean success request: %v", err)
	}
	if !replace {
		t.Fatal("full-history bun install clean success output should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, "[bun install] ok (installed 160 packages; lockfile saved)") ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(body, "bun-package-159") {
		t.Fatalf("bun install clean success output was not archive-backed compacted: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 || summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("stateful-safe bun install clean success should save without structured guard: %+v", summary)
	}
}

func TestWSSStatefulUnsafeBunInstallStaysGuarded(t *testing.T) {
	tests := []struct {
		name         string
		command      string
		output       string
		mustPreserve string
	}{
		{
			name:         "warning",
			command:      "bun install --ignore-scripts",
			output:       "warning: package has a deprecated postinstall\n" + wssBunInstallCleanFixture(12, true),
			mustPreserve: "warning: package has a deprecated postinstall",
		},
		{
			name:         "dry run",
			command:      "bun install --dry-run",
			output:       wssBunInstallCleanFixture(12, true),
			mustPreserve: "bun-package-011",
		},
		{
			name:         "malformed count",
			command:      "bun install --ignore-scripts",
			output:       strings.Replace(wssBunInstallCleanFixture(12, true), "12 packages installed", "11 packages installed", 1),
			mustPreserve: "bun-package-011",
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
			envelope := "Chunk ID: bun-install-unsafe\nWall time: 0.0010 seconds\nProcess exited with code 0\nOriginal token count: 10000\nOutput:\n" +
				tt.output

			env := parseWSJSON(t, wssCommandOutputRequestBody("resp-bun-install-unsafe", "call_bun_install_unsafe", tt.command, envelope, "stateful-bun-install-unsafe-session"))
			replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
			if err != nil {
				t.Fatalf("handle unsafe bun install request: %v", err)
			}
			body := string(env.Body)
			if strings.Contains(body, "[bun install] ok") ||
				strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
				!strings.Contains(body, tt.mustPreserve) {
				t.Fatalf("unsafe bun install output compacted or hid detail: replace=%v body=%s", replace, body)
			}
		})
	}
}

func wssBunInstallCleanFixture(packages int, savedLockfile bool) string {
	var out strings.Builder
	out.WriteString("bun install v1.3.14 (0d9b296a)\n")
	if savedLockfile {
		out.WriteString("Saved lockfile\n")
	}
	out.WriteString("\n")
	for i := 0; i < packages; i++ {
		fmt.Fprintf(&out, "+ bun-package-%03d@1.0.%d\n", i, i)
	}
	out.WriteString("\n")
	fmt.Fprintf(&out, "%d %s installed [9.00ms]\n", packages, wssPluralWord(packages, "package", "packages"))
	return out.String()
}
