package filter

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

func TryCompactTscDiagnostics(argv []string, stdout []byte) ([]byte, bool) {
	if !isTypeScriptDiagnosticArgv(argv) {
		return stdout, false
	}
	compact, _, ok := parseTypeScriptDiagnostics(string(stdout))
	if !ok || len(compact) >= len(stdout) {
		return stdout, false
	}
	return []byte(compact), true
}

func TryCompactKubectlJSON(argv []string, stdout []byte) ([]byte, bool) {
	if !isKubectlJSONArgv(argv) {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" || s[0] != '{' {
		return stdout, false
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal([]byte(s), &root); err != nil {
		return stdout, false
	}
	itemsRaw := root["items"]
	if len(itemsRaw) == 0 {
		itemsRaw = []byte("[" + s + "]")
	}
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(itemsRaw, &items); err != nil || len(items) == 0 {
		return stdout, false
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[kubectl -o json] %d item(s)\n", len(items))
	const maxRows = 24
	rows := 0
	prioritizedItems := kubectlPrioritizedItems(items)
	for _, idx := range cappedEvidenceIndexes(len(prioritizedItems), maxRows, 6) {
		item := prioritizedItems[idx]
		fmt.Fprintf(&b, "  %s\n", compactKubectlJSONItem(item))
		rows++
	}
	if len(items) > rows {
		fmt.Fprintf(&b, "  ... +%d more item(s)\n", len(items)-rows)
	}
	out := b.String()
	if len(out) >= len(stdout) {
		return stdout, false
	}
	return []byte(out), true
}

func TryCompactCargoMetadataJSON(argv []string, stdout []byte) ([]byte, bool) {
	if !isCargoMetadataArgv(argv) {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" || s[0] != '{' {
		return stdout, false
	}
	type pkg struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		ID      string `json:"id"`
	}
	type metadata struct {
		Packages        []pkg    `json:"packages"`
		WorkspaceMember []string `json:"workspace_members"`
		Resolve         *struct {
			Nodes []struct {
				ID           string   `json:"id"`
				Dependencies []string `json:"dependencies"`
			} `json:"nodes"`
		} `json:"resolve"`
	}
	var m metadata
	if err := json.Unmarshal([]byte(s), &m); err != nil || len(m.Packages) == 0 {
		return stdout, false
	}
	workspace := map[string]struct{}{}
	for _, id := range m.WorkspaceMember {
		workspace[id] = struct{}{}
	}
	var workspaceRows []string
	for _, p := range m.Packages {
		if _, ok := workspace[p.ID]; ok {
			workspaceRows = append(workspaceRows, p.Name+" "+p.Version)
		}
	}
	sort.Strings(workspaceRows)
	edges := 0
	if m.Resolve != nil {
		for _, n := range m.Resolve.Nodes {
			edges += len(n.Dependencies)
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[cargo metadata] %d package(s), %d workspace member(s), %d dependency edge(s)\n", len(m.Packages), len(workspaceRows), edges)
	const maxMembers = 16
	emitted := 0
	for _, idx := range cappedEvidenceIndexes(len(workspaceRows), maxMembers, 4) {
		row := workspaceRows[idx]
		fmt.Fprintf(&b, "  %s\n", row)
		emitted++
	}
	if len(workspaceRows) > emitted {
		fmt.Fprintf(&b, "  ... +%d more workspace member(s)\n", len(workspaceRows)-emitted)
	}
	out := b.String()
	if len(out) >= len(stdout) {
		return stdout, false
	}
	return []byte(out), true
}

func TryCompactTerraformShowJSON(argv []string, stdout []byte) ([]byte, bool) {
	if !isTerraformShowJSONArgv(argv) {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" || s[0] != '{' {
		return stdout, false
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal([]byte(s), &root); err != nil {
		return stdout, false
	}
	var b strings.Builder
	if diagnostics := terraformJSONDiagnostics(root["diagnostics"]); len(diagnostics) > 0 {
		fmt.Fprintf(&b, "[terraform show -json] %d diagnostic(s)\n", len(diagnostics))
		for _, row := range diagnostics {
			fmt.Fprintf(&b, "  %s\n", row)
		}
	} else if changes := terraformJSONResourceChanges(root["resource_changes"]); len(changes) > 0 {
		fmt.Fprintf(&b, "[terraform show -json] %d resource change(s)\n", len(changes))
		const maxRows = 30
		emitted := 0
		for _, idx := range selectTerraformJSONChangeIndexes(changes, maxRows) {
			row := changes[idx]
			fmt.Fprintf(&b, "  %s\n", row.text)
			emitted++
		}
		if len(changes) > emitted {
			fmt.Fprintf(&b, "  ... +%d more change(s)\n", len(changes)-emitted)
		}
	} else if resources := terraformJSONStateResources(root); len(resources) > 0 {
		fmt.Fprintf(&b, "[terraform show -json] %d state resource(s)\n", len(resources))
		const maxRows = 30
		emitted := 0
		for _, idx := range cappedEvidenceIndexes(len(resources), maxRows, 6) {
			row := resources[idx]
			fmt.Fprintf(&b, "  %s\n", row)
			emitted++
		}
		if len(resources) > emitted {
			fmt.Fprintf(&b, "  ... +%d more resource(s)\n", len(resources)-emitted)
		}
	} else {
		return stdout, false
	}
	out := b.String()
	if len(out) >= len(stdout) {
		return stdout, false
	}
	return []byte(out), true
}

func isKubectlJSONArgv(argv []string) bool {
	if len(argv) < 2 || !commandMatchesAny(argv, "kubectl", "oc") {
		return false
	}
	for i, a := range argv {
		switch a {
		case "-o=json", "--output=json":
			return true
		case "-o", "--output":
			return i+1 < len(argv) && argv[i+1] == "json"
		}
	}
	return false
}

func isCargoMetadataArgv(argv []string) bool {
	if len(argv) < 2 || !commandMatchesAny(argv, "cargo") {
		return false
	}
	return argv[len(argv)-1] == "metadata" || containsArg(argv[1:], "metadata")
}

func isTerraformShowJSONArgv(argv []string) bool {
	return isTerraformSubcommand(argv, "show") && hasTerraformJSONFlag(argv)
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func kubectlPrioritizedItems(items []map[string]json.RawMessage) []map[string]json.RawMessage {
	out := append([]map[string]json.RawMessage(nil), items...)
	sort.SliceStable(out, func(i, j int) bool {
		return kubectlItemAttention(out[i]) && !kubectlItemAttention(out[j])
	})
	return out
}

func kubectlItemAttention(item map[string]json.RawMessage) bool {
	status := rawObject(item["status"])
	phase := strings.ToLower(rawString(status["phase"]))
	if phase != "" && phase != "running" && phase != "succeeded" && phase != "active" {
		return true
	}
	for _, row := range rawObjectSlice(status["containerStatuses"]) {
		if !rawBool(row["ready"]) || rawFloat(row["restartCount"]) > 0 {
			return true
		}
		state := rawObject(row["state"])
		for _, nested := range state {
			if reason := strings.ToLower(rawString(rawObject(nested)["reason"])); reason != "" && reason != "completed" {
				return true
			}
		}
	}
	for _, cond := range rawObjectSlice(status["conditions"]) {
		if strings.EqualFold(rawString(cond["status"]), "false") || strings.EqualFold(rawString(cond["status"]), "unknown") {
			return true
		}
	}
	return false
}

func compactKubectlJSONItem(item map[string]json.RawMessage) string {
	metadata := rawObject(item["metadata"])
	status := rawObject(item["status"])
	kind := rawString(item["kind"])
	if kind == "" {
		kind = "Item"
	}
	name := rawString(metadata["name"])
	namespace := rawString(metadata["namespace"])
	if namespace != "" {
		name = namespace + "/" + name
	}
	phase := rawString(status["phase"])
	var parts []string
	if phase != "" {
		parts = append(parts, "phase="+phase)
	}
	if rows := rawObjectSlice(status["containerStatuses"]); len(rows) > 0 {
		ready := 0
		restarts := 0
		var reasons []string
		for _, row := range rows {
			if rawBool(row["ready"]) {
				ready++
			}
			restarts += int(rawFloat(row["restartCount"]))
			state := rawObject(row["state"])
			for _, nested := range state {
				if reason := rawString(rawObject(nested)["reason"]); reason != "" && reason != "Completed" {
					reasons = append(reasons, reason)
				}
			}
		}
		parts = append(parts, fmt.Sprintf("ready=%d/%d", ready, len(rows)))
		if restarts > 0 {
			parts = append(parts, fmt.Sprintf("restarts=%d", restarts))
		}
		if len(reasons) > 0 {
			parts = append(parts, "reason="+strings.Join(compactStringSetLocal(reasons), ","))
		}
	}
	for _, cond := range rawObjectSlice(status["conditions"]) {
		condType := rawString(cond["type"])
		condStatus := rawString(cond["status"])
		if condType != "" && condStatus != "" && !strings.EqualFold(condStatus, "true") {
			parts = append(parts, condType+"="+condStatus)
		}
	}
	if len(parts) == 0 {
		return strings.TrimSpace(kind + " " + name)
	}
	return strings.TrimSpace(kind + " " + name + " " + strings.Join(parts, " "))
}

func terraformJSONDiagnostics(raw json.RawMessage) []string {
	var diagnostics []struct {
		Severity string `json:"severity"`
		Summary  string `json:"summary"`
		Detail   string `json:"detail"`
	}
	if err := json.Unmarshal(raw, &diagnostics); err != nil {
		return nil
	}
	var out []string
	for _, d := range diagnostics {
		row := strings.TrimSpace(strings.Join([]string{d.Severity, d.Summary, firstNonEmptyLocal(firstLines(d.Detail, 1)...)}, " "))
		if row != "" {
			out = append(out, row)
		}
	}
	return out
}

type terraformJSONChangeRow struct {
	text     string
	priority int
}

func terraformJSONResourceChanges(raw json.RawMessage) []terraformJSONChangeRow {
	var changes []struct {
		Address string `json:"address"`
		Type    string `json:"type"`
		Name    string `json:"name"`
		Change  struct {
			Actions []string `json:"actions"`
		} `json:"change"`
	}
	if err := json.Unmarshal(raw, &changes); err != nil {
		return nil
	}
	sort.SliceStable(changes, func(i, j int) bool {
		return terraformJSONActionPriority(changes[i].Change.Actions) < terraformJSONActionPriority(changes[j].Change.Actions)
	})
	var out []terraformJSONChangeRow
	for _, c := range changes {
		address := c.Address
		if address == "" {
			address = strings.Trim(c.Type+"."+c.Name, ".")
		}
		actions := strings.Join(c.Change.Actions, ",")
		if actions == "" {
			actions = "no-op"
		}
		out = append(out, terraformJSONChangeRow{
			text:     address + " actions=" + actions,
			priority: terraformJSONActionPriority(c.Change.Actions),
		})
	}
	return out
}

func selectTerraformJSONChangeIndexes(changes []terraformJSONChangeRow, budget int) []int {
	if len(changes) <= budget {
		return cappedEvidenceIndexes(len(changes), budget, 0)
	}
	selected := make([]int, 0, budget)
	remaining := budget
	for priority := 0; priority <= 3 && remaining > 0; priority++ {
		group := make([]int, 0)
		for i, row := range changes {
			if row.priority == priority {
				group = append(group, i)
			}
		}
		if len(group) == 0 {
			continue
		}
		if len(group) <= remaining {
			selected = append(selected, group...)
			remaining -= len(group)
			continue
		}
		if priority <= 1 {
			for _, rel := range cappedEvidenceIndexes(len(group), remaining, 6) {
				selected = append(selected, group[rel])
			}
		} else {
			selected = append(selected, group[:remaining]...)
		}
		remaining = 0
	}
	sort.Ints(selected)
	return selected
}

func terraformJSONActionPriority(actions []string) int {
	for _, action := range actions {
		switch strings.ToLower(strings.TrimSpace(action)) {
		case "delete", "replace":
			return 0
		case "create", "update":
			return 1
		case "read":
			return 2
		}
	}
	return 3
}

func terraformJSONStateResources(root map[string]json.RawMessage) []string {
	values := rawObject(root["values"])
	return terraformJSONModuleResources(rawObject(values["root_module"]), nil)
}

func terraformJSONModuleResources(module map[string]json.RawMessage, out []string) []string {
	for _, res := range rawObjectSlice(module["resources"]) {
		address := rawString(res["address"])
		if address == "" {
			address = strings.Trim(rawString(res["type"])+"."+rawString(res["name"]), ".")
		}
		if address != "" {
			out = append(out, address)
		}
	}
	for _, child := range rawObjectSlice(module["child_modules"]) {
		out = terraformJSONModuleResources(child, out)
	}
	return out
}

func rawObject(raw json.RawMessage) map[string]json.RawMessage {
	var out map[string]json.RawMessage
	_ = json.Unmarshal(raw, &out)
	return out
}

func rawObjectSlice(raw json.RawMessage) []map[string]json.RawMessage {
	var out []map[string]json.RawMessage
	_ = json.Unmarshal(raw, &out)
	return out
}

func rawString(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		return fmt.Sprintf("%.0f", f)
	}
	return ""
}

func rawBool(raw json.RawMessage) bool {
	var b bool
	_ = json.Unmarshal(raw, &b)
	return b
}

func rawFloat(raw json.RawMessage) float64 {
	var f float64
	_ = json.Unmarshal(raw, &f)
	return f
}

func compactStringSetLocal(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func firstNonEmptyLocal(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
