package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/slimference/slimference/internal/planner"
	"github.com/slimference/slimference/internal/tokens"
)

type planInspectFlags struct {
	provider             string
	model                string
	routeMode            string
	inputTokens          int
	outputTokens         int
	taskShape            string
	classes              []string
	disabledLayers       []string
	recentEdit           bool
	layer2Allowed        bool
	layer2Acknowledged   bool
	providerCache        bool
	previousResponse     bool
	outputCooldown       bool
	negativeSavings      bool
	websocketShapeKnown  bool
	websocketMutation    bool
	liveCorpusConfidence string
	latencyBudgetMs      int
	json                 bool
	input                string
}

func handlePlanCmd(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: slimference plan inspect [flags] [-|<request-file>]")
		exitFn(1)
		return
	}
	switch args[0] {
	case "inspect":
		handlePlanInspect(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "plan: unknown subcommand %q (inspect)\n", args[0])
		exitFn(1)
	}
}

func handlePlanInspect(args []string) {
	flags, err := parsePlanInspectArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "plan inspect: %v\n", err)
		exitFn(1)
		return
	}
	bodyTokens, err := planInputTokenEstimate(flags.input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "plan inspect: %v\n", err)
		exitFn(1)
		return
	}
	if flags.inputTokens == 0 {
		flags.inputTokens = bodyTokens
	}
	plan, err := buildInspectablePlan(flags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "plan inspect: %v\n", err)
		exitFn(1)
		return
	}
	if flags.json {
		out, _ := json.MarshalIndent(plan, "", "  ")
		fmt.Println(string(out))
		return
	}
	fmt.Print(renderInspectablePlan(plan))
}

func parsePlanInspectArgs(args []string) (planInspectFlags, error) {
	flags := planInspectFlags{
		provider:             "openai",
		routeMode:            "upstream",
		layer2Allowed:        true,
		layer2Acknowledged:   true,
		liveCorpusConfidence: "unknown",
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--json":
			flags.json = true
		case "--provider":
			i++
			if i >= len(args) || args[i] == "" {
				return flags, fmt.Errorf("--provider requires a value")
			}
			flags.provider = args[i]
		case "--model":
			i++
			if i >= len(args) || args[i] == "" {
				return flags, fmt.Errorf("--model requires a value")
			}
			flags.model = args[i]
		case "--route":
			i++
			if i >= len(args) || args[i] == "" {
				return flags, fmt.Errorf("--route requires a value")
			}
			flags.routeMode = args[i]
		case "--input-tokens":
			value, next, err := parsePlanIntFlag(args, i, "--input-tokens")
			if err != nil {
				return flags, err
			}
			flags.inputTokens = value
			i = next
		case "--output-tokens":
			value, next, err := parsePlanIntFlag(args, i, "--output-tokens")
			if err != nil {
				return flags, err
			}
			flags.outputTokens = value
			i = next
		case "--latency-budget-ms":
			value, next, err := parsePlanIntFlag(args, i, "--latency-budget-ms")
			if err != nil {
				return flags, err
			}
			flags.latencyBudgetMs = value
			i = next
		case "--task-shape":
			i++
			if i >= len(args) || args[i] == "" {
				return flags, fmt.Errorf("--task-shape requires a value")
			}
			flags.taskShape = args[i]
		case "--class":
			i++
			if i >= len(args) || args[i] == "" {
				return flags, fmt.Errorf("--class requires a value")
			}
			flags.classes = append(flags.classes, args[i])
		case "--disable":
			i++
			if i >= len(args) || args[i] == "" {
				return flags, fmt.Errorf("--disable requires a comma-separated layer list")
			}
			flags.disabledLayers = append(flags.disabledLayers, strings.Split(args[i], ",")...)
		case "--recent-edit":
			flags.recentEdit = true
		case "--no-l2-policy":
			flags.layer2Allowed = false
		case "--no-l2-ack":
			flags.layer2Acknowledged = false
		case "--provider-cache":
			flags.providerCache = true
		case "--previous-response":
			flags.previousResponse = true
		case "--output-cooldown":
			flags.outputCooldown = true
		case "--negative-savings":
			flags.negativeSavings = true
		case "--ws-known":
			flags.websocketShapeKnown = true
		case "--ws-mutate":
			flags.websocketMutation = true
		case "--confidence":
			i++
			if i >= len(args) || args[i] == "" {
				return flags, fmt.Errorf("--confidence requires a value")
			}
			flags.liveCorpusConfidence = args[i]
		default:
			if strings.HasPrefix(arg, "--") {
				return flags, fmt.Errorf("unknown flag: %s", arg)
			}
			if flags.input != "" {
				return flags, fmt.Errorf("unexpected extra argument: %s", arg)
			}
			flags.input = arg
		}
	}
	return flags, nil
}

func parsePlanIntFlag(args []string, index int, name string) (int, int, error) {
	next := index + 1
	if next >= len(args) || args[next] == "" {
		return 0, index, fmt.Errorf("%s requires a non-negative integer", name)
	}
	value, err := strconv.Atoi(args[next])
	if err != nil || value < 0 {
		return 0, index, fmt.Errorf("%s requires a non-negative integer", name)
	}
	return value, next, nil
}

func planInputTokenEstimate(input string) (int, error) {
	if input == "" {
		return 0, nil
	}
	var body []byte
	var err error
	if input == "-" {
		body, err = readStdinAll()
	} else {
		body, err = os.ReadFile(input)
	}
	if err != nil {
		return 0, err
	}
	return tokens.CountString(string(body)), nil
}

func buildInspectablePlan(flags planInspectFlags) (planner.CompressionPlan, error) {
	disabled, err := parsePlanDisabledLayers(flags.disabledLayers)
	if err != nil {
		return planner.CompressionPlan{}, err
	}
	return planner.Plan(planner.RequestFacts{
		Provider:                    flags.provider,
		Model:                       flags.model,
		RouteMode:                   flags.routeMode,
		EstimatedInputTokens:        flags.inputTokens,
		ExpectedOutputTokens:        flags.outputTokens,
		TaskShape:                   flags.taskShape,
		ContentClasses:              flags.classes,
		ManualDisabled:              disabled,
		RecentEdit:                  flags.recentEdit,
		ExternalLayer2Allowed:       flags.layer2Allowed,
		Layer2Acknowledged:          flags.layer2Acknowledged,
		ProviderCacheSupported:      flags.providerCache,
		PreviousResponseIDAvailable: flags.previousResponse,
		OutputReduceCooldown:        flags.outputCooldown,
		NegativeSavingsHistory:      flags.negativeSavings,
		WebSocketShapeKnown:         flags.websocketShapeKnown,
		WebSocketMutationRequested:  flags.websocketMutation,
		LiveCorpusConfidence:        flags.liveCorpusConfidence,
		LatencyBudgetMs:             flags.latencyBudgetMs,
	}), nil
}

func parsePlanDisabledLayers(values []string) (map[planner.Layer]bool, error) {
	out := make(map[planner.Layer]bool)
	for _, value := range values {
		layer := strings.ToLower(strings.TrimSpace(value))
		if layer == "" {
			continue
		}
		switch layer {
		case "l0", "0":
			out[planner.Layer0] = true
		case "l1", "1":
			out[planner.Layer1] = true
		case "l2", "2":
			out[planner.Layer2] = true
		case "l3", "3":
			out[planner.Layer3] = true
		case "l4", "4", "output", "output-reduce":
			out[planner.Layer4] = true
		case "websocket", "ws":
			out[planner.LayerWebSocket] = true
		default:
			return nil, fmt.Errorf("unknown layer in --disable: %s", value)
		}
	}
	return out, nil
}

func renderInspectablePlan(plan planner.CompressionPlan) string {
	var sb strings.Builder
	sb.WriteString("Slimference plan inspect\n")
	sb.WriteString(strings.Repeat("=", 60) + "\n")
	sb.WriteString(fmt.Sprintf("Provider:       %s\n", plan.Provider))
	sb.WriteString(fmt.Sprintf("Model:          %s\n", plan.Model))
	sb.WriteString(fmt.Sprintf("Route:          %s\n", plan.RouteMode))
	sb.WriteString(fmt.Sprintf("Safety blocked: %v\n", plan.SafetyBlocked))
	sb.WriteString("Decisions:\n")
	for _, decision := range plan.Decisions {
		sb.WriteString(fmt.Sprintf("  %-10s %-10s save~%-6d risk=%-7s confidence=%-8s %s\n",
			decision.Layer,
			decision.Action,
			decision.ExpectedSavingsTokens,
			decision.Risk,
			decision.Confidence,
			decision.Reason,
		))
	}
	return sb.String()
}
