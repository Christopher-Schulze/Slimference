package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

type wssReferenceInventoryFlags struct {
	path                    string
	outputFormat            string
	requireAcceptedContract bool
	help                    bool
}

type wssReferenceInventoryReport struct {
	Path                        string                       `json:"path"`
	Files                       int                          `json:"files"`
	Lines                       int                          `json:"lines"`
	JSONRows                    int                          `json:"json_rows"`
	ParseErrors                 int                          `json:"parse_errors"`
	FieldKeys                   []wssReferenceInventoryCount `json:"field_keys,omitempty"`
	RawMentions                 []wssReferenceInventoryCount `json:"raw_mentions,omitempty"`
	ReasoningStateFields        []wssReferenceInventoryCount `json:"reasoning_state_fields,omitempty"`
	LocalReferenceURIs          []wssReferenceInventoryCount `json:"local_reference_uris,omitempty"`
	ArbitraryCandidates         []wssReferenceInventoryCount `json:"arbitrary_reference_candidates,omitempty"`
	Lane3AcceptedContractSchema wssReferenceAcceptedSchema   `json:"lane3_accepted_contract_schema"`
	Lane3FieldVerdicts          []wssReferenceFieldVerdict   `json:"lane3_field_verdicts"`
	Lane3AcceptedContracts      int                          `json:"lane3_accepted_contracts"`
	Lane3ReprobeTriggers        []wssReferenceReprobeTrigger `json:"lane3_reprobe_triggers"`
	Verdict                     string                       `json:"verdict"`
	Notes                       []string                     `json:"notes,omitempty"`
}

type wssReferenceInventoryCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type wssReferenceAcceptedSchema struct {
	Version              int      `json:"version"`
	KeyFields            []string `json:"key_fields"`
	AcceptanceSignals    []string `json:"acceptance_signals"`
	FallbackRequirements []string `json:"fallback_requirements"`
	DemotionKey          []string `json:"demotion_key"`
	NegativeAccounting   []string `json:"negative_accounting"`
	ProductInvariants    []string `json:"product_invariants"`
}

type wssReferenceFieldVerdict struct {
	Field              string   `json:"field"`
	Category           string   `json:"category"`
	FieldCount         int      `json:"field_count"`
	RawMentionCount    int      `json:"raw_mention_count"`
	Verdict            string   `json:"verdict"`
	ProductAction      string   `json:"product_action"`
	CandidatePotential string   `json:"candidate_potential"`
	ReprobeTriggers    []string `json:"reprobe_triggers,omitempty"`
}

type wssReferenceReprobeTrigger struct {
	Trigger string `json:"trigger"`
	Action  string `json:"action"`
}

var wssReferenceInventoryKeys = []string{
	"previous_response_id",
	"response_id",
	"item_id",
	"file_id",
	"attachment_id",
	"encrypted_content",
	"reasoning",
	"reasoning_content",
	"reasoning_items",
	"reasoning_summary",
	"reasoning_tokens",
	"thinking",
	"reference_id",
	"content_reference",
	"content_ref",
	"server_reference",
	"server_content_id",
	"conversation_id",
	"thread_id",
	"message_id",
	"call_id",
	"output_id",
	"tool_call_id",
	"id",
}

var wssReferenceInventoryReasoningKeys = []string{
	"encrypted_content",
	"reasoning",
	"reasoning_content",
	"reasoning_items",
	"reasoning_summary",
	"reasoning_tokens",
	"thinking",
}

var wssReferenceInventoryArbitraryKeys = []string{
	"reference_id",
	"content_reference",
	"content_ref",
	"server_reference",
	"server_content_id",
}

var wssReferenceInventoryLocalURIs = []string{
	"local-archive://",
	"slim://archive/",
}

const wssReferenceInventoryHelpText = `wss-reference-inventory: content-free WSS backend-reference field inventory

Usage:
  go run ./scripts/utils wss-reference-inventory <jsonl-or-dir> [--json] [--require-accepted-contract]

Directory mode scans *.json and *.jsonl files recursively. The report counts
only reference-like/reasoning-state JSON field names, raw field-name mentions,
and local archive URI markers. It never prints field values, prompts, tool
output, headers, or payload text. The Lane 3 section emits a versioned
backend-reference contract schema, per-field verdicts, and re-probe triggers
so T408 promotion/kill decisions are explicit instead of hand-wavy.

--require-accepted-contract fails closed unless at least one Lane 3 verdict is
an accepted narrow backend-reference contract. Use it only for release gates
that intentionally require direct backend-reference productization.`

func runWSSReferenceInventory(args []string, stdout, stderr io.Writer) int {
	flags, err := parseWSSReferenceInventoryFlags(args)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 2
	}
	if flags.help {
		fmt.Fprintln(stdout, wssReferenceInventoryHelpText)
		return 0
	}
	if flags.path == "" {
		fmt.Fprintln(stderr, "Usage: wss-reference-inventory <jsonl-or-dir> [--json]")
		return 2
	}
	report, err := loadWSSReferenceInventory(flags.path)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if flags.requireAcceptedContract && report.Lane3AcceptedContracts == 0 {
		fmt.Fprintln(stderr, "wss-reference-inventory: no accepted Lane 3 backend-reference contract for this backend/frame slice")
		return 3
	}
	if flags.outputFormat == outputJSON {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	writeWSSReferenceInventoryText(stdout, report)
	return 0
}

func parseWSSReferenceInventoryFlags(args []string) (wssReferenceInventoryFlags, error) {
	flags := wssReferenceInventoryFlags{outputFormat: outputText}
	for _, arg := range args {
		switch {
		case arg == "--help" || arg == "-h":
			flags.help = true
		case arg == "--json":
			flags.outputFormat = outputJSON
		case arg == "--require-accepted-contract":
			flags.requireAcceptedContract = true
		case strings.HasPrefix(arg, "-"):
			return flags, fmt.Errorf("unknown flag: %s", arg)
		default:
			if flags.path != "" {
				return flags, fmt.Errorf("multiple inventory paths provided")
			}
			flags.path = arg
		}
	}
	return flags, nil
}

func loadWSSReferenceInventory(path string) (wssReferenceInventoryReport, error) {
	files, err := wssReferenceInventoryFiles(path)
	if err != nil {
		return wssReferenceInventoryReport{}, err
	}
	report := wssReferenceInventoryReport{Path: path}
	fieldCounts := make(map[string]int)
	rawCounts := make(map[string]int)
	localCounts := make(map[string]int)
	for _, file := range files {
		stats, err := scanWSSReferenceInventoryFile(file, fieldCounts, rawCounts, localCounts)
		if err != nil {
			return wssReferenceInventoryReport{}, err
		}
		report.Files++
		report.Lines += stats.lines
		report.JSONRows += stats.jsonRows
		report.ParseErrors += stats.parseErrors
	}
	report.FieldKeys = wssReferenceInventoryCounts(fieldCounts)
	report.RawMentions = wssReferenceInventoryCounts(rawCounts)
	report.ReasoningStateFields = wssReferenceInventoryNamedCounts(wssReferenceInventoryReasoningKeys, fieldCounts, rawCounts)
	report.LocalReferenceURIs = wssReferenceInventoryCounts(localCounts)
	report.ArbitraryCandidates = wssReferenceInventoryArbitraryCounts(fieldCounts, rawCounts)
	report.Lane3AcceptedContractSchema = wssReferenceAcceptedContractSchema()
	report.Lane3FieldVerdicts = wssReferenceInventoryFieldVerdicts(fieldCounts, rawCounts, localCounts)
	report.Lane3AcceptedContracts = wssReferenceInventoryAcceptedContractCount(report.Lane3FieldVerdicts)
	report.Lane3ReprobeTriggers = wssReferenceInventoryReprobeTriggers()
	report.Verdict, report.Notes = wssReferenceInventoryVerdict(report)
	return report, nil
}

type wssReferenceInventoryFileStats struct {
	lines       int
	jsonRows    int
	parseErrors int
}

func scanWSSReferenceInventoryFile(path string, fieldCounts, rawCounts, localCounts map[string]int) (wssReferenceInventoryFileStats, error) {
	f, err := os.Open(path)
	if err != nil {
		return wssReferenceInventoryFileStats{}, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	var stats wssReferenceInventoryFileStats
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		stats.lines++
		for _, key := range wssReferenceInventoryKeys {
			if count := countWSSReferenceInventoryRawKey(line, key); count > 0 {
				rawCounts[key] += count
			}
		}
		for _, marker := range wssReferenceInventoryLocalURIs {
			if count := strings.Count(line, marker); count > 0 {
				localCounts[marker] += count
			}
		}

		var value any
		if err := json.Unmarshal([]byte(line), &value); err != nil {
			stats.parseErrors++
			continue
		}
		stats.jsonRows++
		walkWSSReferenceInventoryKeys(value, fieldCounts)
	}
	if err := scanner.Err(); err != nil {
		return wssReferenceInventoryFileStats{}, fmt.Errorf("scan %s: %w", path, err)
	}
	return stats, nil
}

func countWSSReferenceInventoryRawKey(line, key string) int {
	if line == "" || key == "" {
		return 0
	}
	return strings.Count(line, `"`+key+`":`) + strings.Count(line, `\"`+key+`\":`)
}

func walkWSSReferenceInventoryKeys(value any, fieldCounts map[string]int) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			normalized := strings.ToLower(strings.TrimSpace(key))
			if wssReferenceInventoryIsTrackedKey(normalized) {
				fieldCounts[normalized]++
			}
			walkWSSReferenceInventoryKeys(child, fieldCounts)
		}
	case []any:
		for _, child := range typed {
			walkWSSReferenceInventoryKeys(child, fieldCounts)
		}
	}
}

func wssReferenceInventoryIsTrackedKey(key string) bool {
	return slices.Contains(wssReferenceInventoryKeys, key)
}

func wssReferenceInventoryFiles(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}
	if !info.IsDir() {
		return []string{path}, nil
	}
	var files []string
	err = filepath.WalkDir(path, func(child string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if strings.HasSuffix(name, ".jsonl") || strings.HasSuffix(name, ".json") {
			files = append(files, child)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk reference inventory path %s: %w", path, err)
	}
	sort.Strings(files)
	return files, nil
}

func wssReferenceInventoryArbitraryCounts(fieldCounts, rawCounts map[string]int) []wssReferenceInventoryCount {
	return wssReferenceInventoryNamedCounts(wssReferenceInventoryArbitraryKeys, fieldCounts, rawCounts)
}

func wssReferenceInventoryNamedCounts(keys []string, fieldCounts, rawCounts map[string]int) []wssReferenceInventoryCount {
	out := make([]wssReferenceInventoryCount, 0, len(keys)*2)
	for _, key := range keys {
		if count := fieldCounts[key]; count > 0 {
			out = append(out, wssReferenceInventoryCount{Name: "field:" + key, Count: count})
		}
		if count := rawCounts[key]; count > 0 {
			out = append(out, wssReferenceInventoryCount{Name: "raw:" + key, Count: count})
		}
	}
	sortWSSReferenceInventoryCounts(out)
	return out
}

func wssReferenceInventoryCounts(counts map[string]int) []wssReferenceInventoryCount {
	out := make([]wssReferenceInventoryCount, 0, len(counts))
	for name, count := range counts {
		if count <= 0 {
			continue
		}
		out = append(out, wssReferenceInventoryCount{Name: name, Count: count})
	}
	sortWSSReferenceInventoryCounts(out)
	return out
}

func sortWSSReferenceInventoryCounts(rows []wssReferenceInventoryCount) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Count != rows[j].Count {
			return rows[i].Count > rows[j].Count
		}
		return rows[i].Name < rows[j].Name
	})
}

func wssReferenceInventoryVerdict(report wssReferenceInventoryReport) (string, []string) {
	notes := []string{
		"previous_response_id is a server continuation anchor, not an arbitrary block-reference contract.",
		"file_id and attachment_id are scoped upload/attachment identifiers; they do not prove that arbitrary prior text blocks can be referenced.",
		"local-archive:// and slim://archive/ are Slimference-local recovery references; they are not backend-honored unless rehydrated before upstream visibility.",
	}
	if len(report.ReasoningStateFields) > 0 {
		notes = append(notes, "Reasoning/encrypted state fields were observed; they are Class-D ceiling mass, not product-safe direct savings without a backend-honored exact reference.")
	} else {
		notes = append(notes, "No tracked reasoning/encrypted state field was observed in this inventory slice.")
	}
	if len(report.ArbitraryCandidates) == 0 {
		notes = append(notes, "No arbitrary backend reference field was observed; T408 product mutation must remain off.")
		return "no_arbitrary_backend_reference_observed", notes
	}
	notes = append(notes, "Arbitrary-looking reference fields were observed; this is a contract-discovery candidate only and still needs live byte-equal fallback proof.")
	return "arbitrary_reference_candidate_observed", notes
}

func wssReferenceAcceptedContractSchema() wssReferenceAcceptedSchema {
	return wssReferenceAcceptedSchema{
		Version: 1,
		KeyFields: []string{
			"backend version",
			"Codex version",
			"route",
			"request shape",
			"lineage mode",
			"reference field",
			"content class",
		},
		AcceptanceSignals: []string{
			"backend accepts the reference field without invalid_request or 400",
			"downstream state behaves byte-equivalent to the full original payload",
			"the model can use every referenced byte as if the original text was present",
			"cache-prefix behavior is not worse than the byte-equal baseline for the accepted slice",
			"S_local is positive after fallback, retry, and recovery costs",
		},
		FallbackRequirements: []string{
			"exact archived raw bytes",
			"raw hash and byte-size validation",
			"session and lineage match",
			"rehydrate-before-upstream fallback on rejection or drift",
			"fail-open to original bytes on missing archive or validator mismatch",
		},
		DemotionKey: []string{
			"backend version",
			"Codex version",
			"route",
			"request shape",
			"lineage mode",
			"reference field",
			"content class",
		},
		NegativeAccounting: []string{
			"fallback rehydration bytes",
			"retry bytes",
			"manual expand bytes",
			"cache-bust or provider-cache loss attributed separately from S_local",
		},
		ProductInvariants: []string{
			"no local-only reference may be model-visible",
			"no local-only reference may be upstream-visible unless the accepted contract explicitly covers it",
			"neighboring unaccepted route/request shapes remain byte-equal",
			"any invalid_request, 400, lost state, response_failed, or context-loss signal demotes the exact slice",
		},
	}
}

func wssReferenceInventoryFieldVerdicts(fieldCounts, rawCounts, localCounts map[string]int) []wssReferenceFieldVerdict {
	verdicts := []wssReferenceFieldVerdict{
		wssReferenceInventoryFieldVerdict(fieldCounts, rawCounts, "previous_response_id", "continuation_anchor", "rejected_continuation_anchor_only", "Keep as server-state lineage metadata; do not treat it as an arbitrary text-block reference.", "0 direct points"),
		wssReferenceInventoryFieldVerdict(fieldCounts, rawCounts, "response_id", "response_identifier", "rejected_response_identifier_only", "Use only for lineage/accounting correlation unless a future contract binds it to byte-exact content references.", "0 direct points"),
		wssReferenceInventoryFieldVerdict(fieldCounts, rawCounts, "item_id", "item_identifier", "rejected_item_identifier_only", "Use only as scoped item metadata; do not elide arbitrary content behind it.", "0 direct points"),
		wssReferenceInventoryFieldVerdict(fieldCounts, rawCounts, "file_id", "upload_identifier", "rejected_scoped_upload_identifier_only", "Use only for uploaded/file attachment flows; not a proof of arbitrary history block references.", "0 direct points"),
		wssReferenceInventoryFieldVerdict(fieldCounts, rawCounts, "attachment_id", "attachment_identifier", "rejected_scoped_attachment_identifier_only", "Use only for attachment metadata; not a general prior-text reference.", "0 direct points"),
		wssReferenceInventoryFieldVerdict(fieldCounts, rawCounts, "encrypted_content", "reasoning_or_encrypted_state", "rejected_class_d_no_direct_mutation", "Treat as ceiling mass and leave byte-equal unless a separate backend contract explicitly exposes exact recoverable state.", "0 direct points"),
		wssReferenceInventoryFieldVerdict(fieldCounts, rawCounts, "reasoning", "reasoning_or_encrypted_state", "rejected_class_d_no_direct_mutation", "Treat as reasoning-state metadata; do not compress or synthesize.", "0 direct points"),
		wssReferenceInventoryFieldVerdict(fieldCounts, rawCounts, "reasoning_content", "reasoning_or_encrypted_state", "rejected_class_d_no_direct_mutation", "Treat as reasoning-state metadata; do not compress or synthesize.", "0 direct points"),
		wssReferenceInventoryFieldVerdict(fieldCounts, rawCounts, "reasoning_items", "reasoning_or_encrypted_state", "rejected_class_d_no_direct_mutation", "Treat as reasoning-state metadata; do not compress or synthesize.", "0 direct points"),
		wssReferenceInventoryFieldVerdict(fieldCounts, rawCounts, "reasoning_summary", "reasoning_or_encrypted_state", "rejected_class_d_no_direct_mutation", "Treat as reasoning-state metadata; do not compress or synthesize.", "0 direct points"),
		wssReferenceInventoryFieldVerdict(fieldCounts, rawCounts, "reasoning_tokens", "reasoning_or_encrypted_state", "rejected_class_d_no_direct_mutation", "Treat as accounting metadata only.", "0 direct points"),
		wssReferenceInventoryFieldVerdict(fieldCounts, rawCounts, "thinking", "reasoning_or_encrypted_state", "rejected_class_d_no_direct_mutation", "Treat as reasoning-state metadata; do not compress or synthesize.", "0 direct points"),
	}
	for _, field := range wssReferenceInventoryArbitraryKeys {
		fieldCount := fieldCounts[field]
		rawCount := rawCounts[field]
		verdict := "not_observed_current_slice"
		action := "Do not create a live probe until this field is observed in the current backend/frame schema or appears in an official contract."
		potential := "0 current points; +10 to +30 local points if a narrow backend-honored contract is later accepted"
		if fieldCount+rawCount > 0 {
			verdict = "candidate_needs_isolated_acceptance_probe"
			action = "Create a narrow lab probe for this exact field and request shape; product remains byte-equal until acceptance, fallback, demotion, and positive S_local are satisfied."
		}
		verdicts = append(verdicts, wssReferenceFieldVerdict{
			Field:              field,
			Category:           "arbitrary_reference_candidate",
			FieldCount:         fieldCount,
			RawMentionCount:    rawCount,
			Verdict:            verdict,
			ProductAction:      action,
			CandidatePotential: potential,
			ReprobeTriggers:    wssReferenceInventoryReprobeTriggerNames(),
		})
	}
	for _, marker := range wssReferenceInventoryLocalURIs {
		verdicts = append(verdicts, wssReferenceFieldVerdict{
			Field:              marker,
			Category:           "local_archive_reference_uri",
			FieldCount:         localCounts[marker],
			Verdict:            "rejected_local_only_rehydrate_before_upstream",
			ProductAction:      "Use only inside Slimference or after exact rehydration before upstream/model visibility.",
			CandidatePotential: "0 direct backend-reference points; enabling value for Lane 2 recovery/accounting",
		})
	}
	sort.SliceStable(verdicts, func(i, j int) bool {
		if verdicts[i].Category != verdicts[j].Category {
			return verdicts[i].Category < verdicts[j].Category
		}
		return verdicts[i].Field < verdicts[j].Field
	})
	return verdicts
}

func wssReferenceInventoryFieldVerdict(fieldCounts, rawCounts map[string]int, field, category, verdict, action, potential string) wssReferenceFieldVerdict {
	return wssReferenceFieldVerdict{
		Field:              field,
		Category:           category,
		FieldCount:         fieldCounts[field],
		RawMentionCount:    rawCounts[field],
		Verdict:            verdict,
		ProductAction:      action,
		CandidatePotential: potential,
	}
}

func wssReferenceInventoryAcceptedContractCount(rows []wssReferenceFieldVerdict) int {
	count := 0
	for _, row := range rows {
		if row.Verdict == "accepted_narrow_backend_reference_contract" {
			count++
		}
	}
	return count
}

func wssReferenceInventoryReprobeTriggers() []wssReferenceReprobeTrigger {
	return []wssReferenceReprobeTrigger{
		{Trigger: "backend_version_change", Action: "rerun inventory and rebuild field verdicts before considering any accepted slice"},
		{Trigger: "codex_version_change", Action: "rerun inventory and compare route/request-shape/lineage fields"},
		{Trigger: "frame_schema_change", Action: "rerun inventory immediately when new reference-like fields appear"},
		{Trigger: "official_api_reference_contract", Action: "create a narrow acceptance probe for the documented field and content class"},
		{Trigger: "new_reference_like_live_field", Action: "create a candidate record with byte-equal fallback and demotion key before any mutation"},
	}
}

func wssReferenceInventoryReprobeTriggerNames() []string {
	triggers := wssReferenceInventoryReprobeTriggers()
	names := make([]string, 0, len(triggers))
	for _, trigger := range triggers {
		names = append(names, trigger.Trigger)
	}
	return names
}

func writeWSSReferenceInventoryText(w io.Writer, report wssReferenceInventoryReport) {
	fmt.Fprintf(w, "=== WSS Reference Inventory: %s ===\n", filepath.Base(report.Path))
	fmt.Fprintf(w, "Files:               %d\n", report.Files)
	fmt.Fprintf(w, "Lines:               %d\n", report.Lines)
	fmt.Fprintf(w, "JSON rows:           %d\n", report.JSONRows)
	fmt.Fprintf(w, "Parse errors:        %d\n", report.ParseErrors)
	fmt.Fprintf(w, "Verdict:             %s\n", report.Verdict)
	fmt.Fprintf(w, "Lane3 accepted:      %d\n", report.Lane3AcceptedContracts)
	writeWSSReferenceInventoryRows(w, "\nTracked field keys:", report.FieldKeys)
	writeWSSReferenceInventoryRows(w, "\nRaw mentions:", report.RawMentions)
	writeWSSReferenceInventoryRows(w, "\nReasoning/encrypted state fields:", report.ReasoningStateFields)
	writeWSSReferenceInventoryRows(w, "\nLocal reference URIs:", report.LocalReferenceURIs)
	writeWSSReferenceInventoryRows(w, "\nArbitrary candidates:", report.ArbitraryCandidates)
	fmt.Fprintf(w, "\nLane 3 accepted-contract schema: v%d\n", report.Lane3AcceptedContractSchema.Version)
	writeWSSReferenceStringRows(w, "  key fields:", report.Lane3AcceptedContractSchema.KeyFields)
	writeWSSReferenceFieldVerdicts(w, report.Lane3FieldVerdicts)
	writeWSSReferenceReprobeTriggers(w, report.Lane3ReprobeTriggers)
	if len(report.Notes) > 0 {
		fmt.Fprintln(w, "\nNotes:")
		for _, note := range report.Notes {
			fmt.Fprintf(w, "  - %s\n", note)
		}
	}
}

func writeWSSReferenceStringRows(w io.Writer, title string, rows []string) {
	if len(rows) == 0 {
		return
	}
	fmt.Fprintln(w, title)
	for _, row := range rows {
		fmt.Fprintf(w, "    - %s\n", row)
	}
}

func writeWSSReferenceFieldVerdicts(w io.Writer, rows []wssReferenceFieldVerdict) {
	if len(rows) == 0 {
		return
	}
	fmt.Fprintln(w, "\nLane 3 field verdicts:")
	for _, row := range rows {
		if row.FieldCount == 0 && row.RawMentionCount == 0 && row.Category != "arbitrary_reference_candidate" {
			continue
		}
		fmt.Fprintf(w, "  %-28s %-40s fields=%d raw=%d\n", row.Field, row.Verdict, row.FieldCount, row.RawMentionCount)
	}
}

func writeWSSReferenceReprobeTriggers(w io.Writer, rows []wssReferenceReprobeTrigger) {
	if len(rows) == 0 {
		return
	}
	fmt.Fprintln(w, "\nLane 3 re-probe triggers:")
	for _, row := range rows {
		fmt.Fprintf(w, "  - %s: %s\n", row.Trigger, row.Action)
	}
}

func writeWSSReferenceInventoryRows(w io.Writer, title string, rows []wssReferenceInventoryCount) {
	if len(rows) == 0 {
		return
	}
	fmt.Fprintln(w, title)
	for _, row := range rows {
		fmt.Fprintf(w, "  %-28s %d\n", row.Name, row.Count)
	}
}
