package filter

import (
	"strings"
	"testing"
)

func TestLayer0ReducerRegistryContracts(t *testing.T) {
	t.Parallel()
	registry := Layer0ReducerRegistry()
	if len(registry) == 0 {
		t.Fatal("registry is empty")
	}

	seen := make(map[string]bool, len(registry))
	for _, reducer := range registry {
		if reducer.ID == "" {
			t.Fatalf("reducer with empty id: %+v", reducer)
		}
		if seen[reducer.ID] {
			t.Fatalf("duplicate reducer id %q", reducer.ID)
		}
		seen[reducer.ID] = true
		if reducer.Family == "" {
			t.Fatalf("%s has empty family", reducer.ID)
		}
		if reducer.SafetyClass == "" {
			t.Fatalf("%s has empty safety class", reducer.ID)
		}
		if !reducer.DefaultEligible {
			t.Fatalf("%s is in product dispatch order but not default eligible", reducer.ID)
		}
		if len(reducer.PreservedEvidence) == 0 {
			t.Fatalf("%s has no preserved evidence contract", reducer.ID)
		}
		if len(reducer.RequiredFields) == 0 {
			t.Fatalf("%s has no required fields contract", reducer.ID)
		}
		if reducer.RecoveryPath == "" {
			t.Fatalf("%s has no recovery path contract", reducer.ID)
		}
		if reducer.SafetyClass == Layer0ReducerSafetyEmptyEvidence {
			if reducer.ID != "ls" && reducer.ID != "tree" {
				t.Fatalf("%s uses empty-evidence safety class; only empty filesystem probes may use it", reducer.ID)
			}
			if !containsPreservedEvidence(reducer.PreservedEvidence, "full-pass") {
				t.Fatalf("%s empty-evidence reducer must declare non-empty full-pass behavior: %+v", reducer.ID, reducer.PreservedEvidence)
			}
		}
	}

	for _, id := range []string{
		"tier1_sarif",
		"git_status",
		"git_ls_files",
		"log_duplicate_runs",
		"build_output",
		"test_output",
		"search_output",
		"log_output",
		"vcs_host_json_exact",
		"known_cli_json_exact",
		"jq_json_exact",
		"json_minify",
	} {
		if !seen[id] {
			t.Fatalf("registry missing %s", id)
		}
	}
}

func containsPreservedEvidence(values []string, needle string) bool {
	for _, value := range values {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func TestLayer0ReducerRegistryOrder(t *testing.T) {
	t.Parallel()
	registry := Layer0ReducerRegistry()
	position := make(map[string]int, len(registry))
	for i, reducer := range registry {
		position[reducer.ID] = i
	}

	if position["tier1_sarif"] > position["git_status"] {
		t.Fatal("tier1 reducers must run before heuristic reducers")
	}
	if position["log_duplicate_runs"] > position["test_output"] {
		t.Fatal("exact log duplicate reducer must run before generic diagnostic reducers")
	}
	if position["test_output"] > position["build_output"] {
		t.Fatal("dedicated test reducer must run before build reducer fallback")
	}
	if position["build_output"] > position["log_output"] {
		t.Fatal("dedicated build reducer must run before generic log reducer")
	}
	if position["test_output"] > position["log_output"] {
		t.Fatal("dedicated test reducer must run before generic log reducer")
	}
	if position["vcs_host_json_exact"] > position["json_minify"] {
		t.Fatal("VCS host exact JSON reducer must run before generic JSON reducer")
	}
	if position["known_cli_json_exact"] > position["json_minify"] {
		t.Fatal("known CLI exact JSON reducer must run before generic JSON reducer")
	}
	if position["jq_json_exact"] > position["json_minify"] {
		t.Fatal("jq exact JSON reducer must run before generic JSON reducer")
	}
}

func TestLayer0ReducerRegistryReturnsCopy(t *testing.T) {
	t.Parallel()
	first := Layer0ReducerRegistry()
	if len(first) == 0 || len(first[0].PreservedEvidence) == 0 {
		t.Fatal("registry unexpectedly empty")
	}
	first[0].ID = "mutated"
	first[0].RequiredFields[0] = "mutated"
	first[0].PreservedEvidence[0] = "mutated"

	second := Layer0ReducerRegistry()
	if second[0].ID == "mutated" {
		t.Fatal("registry id was mutated through returned slice")
	}
	if second[0].RequiredFields[0] == "mutated" {
		t.Fatal("registry required fields were mutated through returned slice")
	}
	if second[0].PreservedEvidence[0] == "mutated" {
		t.Fatal("registry evidence was mutated through returned slice")
	}
}
