package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type wssReferenceInventoryFlags struct {
	path         string
	outputFormat string
	help         bool
}

type wssReferenceInventoryReport struct {
	Path                 string                       `json:"path"`
	Files                int                          `json:"files"`
	Lines                int                          `json:"lines"`
	JSONRows             int                          `json:"json_rows"`
	ParseErrors          int                          `json:"parse_errors"`
	FieldKeys            []wssReferenceInventoryCount `json:"field_keys,omitempty"`
	RawMentions          []wssReferenceInventoryCount `json:"raw_mentions,omitempty"`
	ReasoningStateFields []wssReferenceInventoryCount `json:"reasoning_state_fields,omitempty"`
	LocalReferenceURIs   []wssReferenceInventoryCount `json:"local_reference_uris,omitempty"`
	ArbitraryCandidates  []wssReferenceInventoryCount `json:"arbitrary_reference_candidates,omitempty"`
	Verdict              string                       `json:"verdict"`
	Notes                []string                     `json:"notes,omitempty"`
}

type wssReferenceInventoryCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
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
  go run ./scripts/utils wss-reference-inventory <jsonl-or-dir> [--json]

Directory mode scans *.json and *.jsonl files recursively. The report counts
only reference-like/reasoning-state JSON field names, raw field-name mentions,
and local archive URI markers. It never prints field values, prompts, tool
output, headers, or payload text. Use it for T408 backend-reference discovery
and T416 reasoning/encrypted-context ceiling proof before any server-state or
reasoning-state promotion.`

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
	for _, tracked := range wssReferenceInventoryKeys {
		if key == tracked {
			return true
		}
	}
	return false
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

func writeWSSReferenceInventoryText(w io.Writer, report wssReferenceInventoryReport) {
	fmt.Fprintf(w, "=== WSS Reference Inventory: %s ===\n", filepath.Base(report.Path))
	fmt.Fprintf(w, "Files:               %d\n", report.Files)
	fmt.Fprintf(w, "Lines:               %d\n", report.Lines)
	fmt.Fprintf(w, "JSON rows:           %d\n", report.JSONRows)
	fmt.Fprintf(w, "Parse errors:        %d\n", report.ParseErrors)
	fmt.Fprintf(w, "Verdict:             %s\n", report.Verdict)
	writeWSSReferenceInventoryRows(w, "\nTracked field keys:", report.FieldKeys)
	writeWSSReferenceInventoryRows(w, "\nRaw mentions:", report.RawMentions)
	writeWSSReferenceInventoryRows(w, "\nReasoning/encrypted state fields:", report.ReasoningStateFields)
	writeWSSReferenceInventoryRows(w, "\nLocal reference URIs:", report.LocalReferenceURIs)
	writeWSSReferenceInventoryRows(w, "\nArbitrary candidates:", report.ArbitraryCandidates)
	if len(report.Notes) > 0 {
		fmt.Fprintln(w, "\nNotes:")
		for _, note := range report.Notes {
			fmt.Fprintf(w, "  - %s\n", note)
		}
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
