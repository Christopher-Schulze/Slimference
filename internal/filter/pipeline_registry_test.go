package filter

import "testing"

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
	}

	for _, id := range []string{
		"tier1_sarif",
		"git_status",
		"build_output",
		"test_output",
		"search_output",
		"log_output",
		"json_minify",
	} {
		if !seen[id] {
			t.Fatalf("registry missing %s", id)
		}
	}
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
	if position["build_output"] > position["log_output"] {
		t.Fatal("dedicated build reducer must run before generic log reducer")
	}
	if position["test_output"] > position["log_output"] {
		t.Fatal("dedicated test reducer must run before generic log reducer")
	}
}

func TestLayer0ReducerRegistryReturnsCopy(t *testing.T) {
	t.Parallel()
	first := Layer0ReducerRegistry()
	if len(first) == 0 || len(first[0].PreservedEvidence) == 0 {
		t.Fatal("registry unexpectedly empty")
	}
	first[0].ID = "mutated"
	first[0].PreservedEvidence[0] = "mutated"

	second := Layer0ReducerRegistry()
	if second[0].ID == "mutated" {
		t.Fatal("registry id was mutated through returned slice")
	}
	if second[0].PreservedEvidence[0] == "mutated" {
		t.Fatal("registry evidence was mutated through returned slice")
	}
}
