package proxy

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/config"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
)

func TestWSSStatefulSafeTerraformInitCleanSuccessCompactsReconnectFullHistoryTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	adapter.setSocketSeq(2)

	envelope := "Chunk ID: terraform-init-safe\n" +
		"Wall time: 2.1000 seconds\n" +
		"Process exited with code 0\n" +
		"Original token count: 10000\n" +
		"Output:\n" +
		wssTerraformInitSuccessFixture(80, 5)
	env := parseWSJSON(t, wssCommandOutputRequestBody(
		"resp-terraform-init-safe",
		"call_terraform_init_safe",
		"terraform init -input=false",
		envelope,
		"stateful-terraform-init-safe-session",
	))

	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle terraform init clean request: %v", err)
	}
	if !replace {
		t.Fatal("reconnect full-history terraform init clean output should compact")
	}
	body := string(env.Body)
	if !strings.Contains(body, "Terraform has been successfully initialized!") ||
		!strings.Contains(body, "- 80 provider(s) installed") ||
		!strings.Contains(body, "- 5 module(s) downloaded") ||
		!strings.Contains(body, "- lock file created: .terraform.lock.hcl") ||
		!strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		strings.Contains(body, "hashicorp/provider079") {
		t.Fatalf("terraform init output was not archive-backed compacted without losing key facts: %s", body)
	}
	summary := p.DebugRecorder().Last(1, false)[0]
	if summary.Tokens.Saved <= 0 ||
		summary.DebugFacts["wss.structured_mutation_guard"] != "" ||
		summary.DebugFacts["wss.request_shape"] != "full_history" {
		t.Fatalf("stateful-safe terraform init should save without structured guard: %+v", summary)
	}
}

func TestWSSCompactedTerraformInitSuccessRejectsDiagnosticsAndUnknownShapes(t *testing.T) {
	t.Parallel()
	accept := []byte(strings.Join([]string{
		"Initializing the backend...",
		"Initializing provider plugins...",
		"Terraform has been successfully initialized!",
		"- 4 provider(s) installed",
		"- 2 module(s) downloaded",
		"- lock file created: .terraform.lock.hcl",
	}, "\n"))
	if !wssCompactedTerraformInitSuccess(accept) {
		t.Fatalf("clean terraform init summary should be accepted: %q", accept)
	}

	rejects := [][]byte{
		[]byte("Terraform has been successfully initialized!\n| Warning: Provider development overrides are in effect\n"),
		[]byte("Terraform has been successfully initialized!\n| Error: Failed to install provider\n"),
		[]byte("Terraform has been successfully initialized!\n- provider(s) installed\n"),
		[]byte("Terraform has been successfully initialized!\nTerraform has created a lock file .terraform.lock.hcl\n"),
		[]byte("Initializing provider plugins...\n- 2 provider(s) installed\n"),
	}
	for _, compacted := range rejects {
		compacted := compacted
		t.Run(string(compacted), func(t *testing.T) {
			t.Parallel()
			if wssCompactedTerraformInitSuccess(compacted) {
				t.Fatalf("unsafe terraform init summary accepted: %q", compacted)
			}
		})
	}
}

func TestWSSStatefulSafeTerraformInitWarningStaysGuarded(t *testing.T) {
	cfg := config.Defaults()
	cfg.Compression.OutputReduce.StopSequencesEnabled = false
	cfg.Compression.OutputReduce.BeTerseHintEnabled = false
	cfg.Compression.OutputReduce.StaleReadAgingEnabled = false
	cfg.Compression.OutputReduce.ObsoleteReadPruneEnabled = false
	p := New(cfg)
	adapter := (&PhaseFDispatcher{Proxy: p}).newWSPhaseFAdapter()
	adapter.setSocketSeq(2)

	output := strings.Join([]string{
		"Initializing the backend...",
		"",
		"| Warning: Provider development overrides are in effect",
		"|",
		"| The provider installation behavior is overridden by CLI configuration.",
		"",
		"Initializing provider plugins...",
		"- Finding hashicorp/aws versions matching \"~> 5.0\"...",
		"- Installing hashicorp/aws v5.31.0...",
		"- Installed hashicorp/aws v5.31.0 (signed by HashiCorp)",
		"",
		"Terraform has been successfully initialized!",
		"",
	}, "\n")
	envelope := "Chunk ID: terraform-init-warning\n" +
		"Wall time: 2.1000 seconds\n" +
		"Process exited with code 0\n" +
		"Original token count: 10000\n" +
		"Output:\n" +
		output
	env := parseWSJSON(t, wssCommandOutputRequestBody(
		"resp-terraform-init-warning",
		"call_terraform_init_warning",
		"terraform init -input=false",
		envelope,
		"stateful-terraform-init-warning-session",
	))

	replace, err := adapter.handle(context.Background(), wsmitm.DirClientToServer, &env)
	if err != nil {
		t.Fatalf("handle terraform init warning request: %v", err)
	}
	body := string(env.Body)
	if replace ||
		strings.Contains(body, "[context-archive kind=tool-output uri=local-archive://") ||
		!strings.Contains(body, "Provider development overrides are in effect") {
		t.Fatalf("terraform init warning output must stay byte-visible, replace=%v body=%s", replace, body)
	}
}

func wssTerraformInitSuccessFixture(providers, modules int) string {
	var out strings.Builder
	out.WriteString("Initializing modules...\n")
	for i := 0; i < modules; i++ {
		fmt.Fprintf(&out, "Downloading registry.terraform.io/example/module%03d/aws 1.%d.0 for module module%03d...\n", i, i, i)
	}
	out.WriteString("\nInitializing the backend...\n\n")
	out.WriteString("Initializing provider plugins...\n")
	for i := 0; i < providers; i++ {
		fmt.Fprintf(&out, "- Finding hashicorp/provider%03d versions matching \"~> 1.%d\"...\n", i, i)
		fmt.Fprintf(&out, "- Installing hashicorp/provider%03d v1.%d.0...\n", i, i)
	}
	out.WriteString("\nTerraform has created a lock file .terraform.lock.hcl to record the provider\n")
	out.WriteString("selections it made above. Include this file in your version control repository.\n\n")
	out.WriteString("Terraform has been successfully initialized!\n")
	return out.String()
}
