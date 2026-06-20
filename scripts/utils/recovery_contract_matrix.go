package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/Christopher-Schulze/Slimference/internal/filter"
)

type recoveryContractMatrixFlags struct {
	outputFormat      string
	failOnProductGaps bool
	help              bool
}

type recoveryContractMatrixReport struct {
	Rows             []recoveryContractRow     `json:"rows"`
	Summary          recoveryContractSummary   `json:"summary"`
	ProductGapRows   []recoveryContractGapView `json:"product_gap_rows,omitempty"`
	BlockedRows      []recoveryContractGapView `json:"blocked_rows,omitempty"`
	HighImpactNext   []string                  `json:"high_impact_next"`
	AcceptancePolicy []string                  `json:"acceptance_policy"`
}

type recoveryContractSummary struct {
	Rows                  int `json:"rows"`
	ProductReady          int `json:"product_ready"`
	ProductGaps           int `json:"product_gaps"`
	BlockedRows           int `json:"blocked_rows"`
	ArchiveBackedRows     int `json:"archive_backed_rows"`
	RehydrateBeforeRows   int `json:"rehydrate_before_upstream_rows"`
	Layer0RegistryRows    int `json:"layer0_registry_rows"`
	CommandOutputRows     int `json:"command_output_first_rows"`
	ServerStateLaneRows   int `json:"server_state_lane_rows"`
	ManualRecoveryRows    int `json:"manual_recovery_rows"`
	ResearchRows          int `json:"research_rows"`
	DefaultEligibleRows   int `json:"default_eligible_rows"`
	NonDefaultBlockedRows int `json:"non_default_blocked_rows"`
}

type recoveryContractGapView struct {
	ID       string   `json:"id"`
	Blockers []string `json:"blockers"`
}

type recoveryContractRow struct {
	ID                      string   `json:"id"`
	Lane                    string   `json:"lane"`
	Surface                 string   `json:"surface"`
	ContentClass            string   `json:"content_class"`
	OmittedBytes            string   `json:"omitted_bytes"`
	DefaultEligible         bool     `json:"default_eligible"`
	ProductReady            bool     `json:"product_ready"`
	ModelVisible            bool     `json:"model_visible"`
	UpstreamVisible         bool     `json:"upstream_visible"`
	InternalOnly            bool     `json:"internal_only"`
	RehydrateBeforeUpstream bool     `json:"rehydrate_before_upstream"`
	DeterministicParser     bool     `json:"deterministic_parser"`
	ArchiveExact            bool     `json:"archive_exact"`
	MechanicalRecovery      bool     `json:"mechanical_recovery"`
	NegativeAccounting      bool     `json:"negative_accounting"`
	FailOpen                bool     `json:"fail_open"`
	RecoveryPath            string   `json:"recovery_path"`
	AccountingPath          string   `json:"accounting_path"`
	RequiredFields          []string `json:"required_fields,omitempty"`
	PreservedEvidence       []string `json:"preserved_evidence,omitempty"`
	FailOpenPredicates      []string `json:"fail_open_predicates,omitempty"`
	EstimatedImpact         string   `json:"estimated_impact"`
	Blockers                []string `json:"blockers,omitempty"`
	NextAction              string   `json:"next_action"`
}

const recoveryContractMatrixHelp = `recovery-contract-matrix: T419 archive/recovery contract ledger

Usage:
  go run ./scripts/utils recovery-contract-matrix [--json|--fail-on-product-gaps]

The report is content-free. It inventories current recovery-capable surfaces,
Layer-0 parser reducers, command-output-first archive-backed classes, WSS/HTTP
rehydration paths, and research lanes. Product rows must declare deterministic
evidence, fail-open behavior, exact archive/recovery when bytes are omitted,
and negative accounting when recovery expands hidden bytes.`

func runRecoveryContractMatrix(args []string, stdout, stderr io.Writer) int {
	flags, err := parseRecoveryContractMatrixFlags(args)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 2
	}
	if flags.help {
		fmt.Fprintln(stdout, recoveryContractMatrixHelp)
		return 0
	}
	report := buildRecoveryContractMatrixReport()
	if flags.outputFormat == outputJSON {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		fmt.Fprintln(stdout, string(data))
	} else {
		writeRecoveryContractMatrixText(stdout, report)
	}
	if flags.failOnProductGaps && report.Summary.ProductGaps > 0 {
		fmt.Fprintf(stderr, "recovery-contract-matrix: %d product gap rows\n", report.Summary.ProductGaps)
		return 1
	}
	return 0
}

func parseRecoveryContractMatrixFlags(args []string) (recoveryContractMatrixFlags, error) {
	flags := recoveryContractMatrixFlags{outputFormat: outputText}
	for _, arg := range args {
		switch arg {
		case "--json":
			if flags.outputFormat != outputText {
				return flags, fmt.Errorf("multiple output flags provided")
			}
			flags.outputFormat = outputJSON
		case "--fail-on-product-gaps":
			flags.failOnProductGaps = true
		case "--help", "-h":
			flags.help = true
		case "":
			continue
		default:
			if strings.HasPrefix(arg, "-") {
				return flags, fmt.Errorf("unknown flag: %s", arg)
			}
			return flags, fmt.Errorf("unexpected argument: %s", arg)
		}
	}
	return flags, nil
}

func buildRecoveryContractMatrixReport() recoveryContractMatrixReport {
	rows := make([]recoveryContractRow, 0, len(filter.Layer0ReducerRegistry())+12)
	rows = append(rows, layer0RecoveryContractRows()...)
	rows = append(rows,
		commandOutputFirstArchiveRow("t418_command_output_first_archive_stdout", "stdout"),
		commandOutputFirstArchiveRow("t418_command_output_first_archive_stderr", "stderr"),
		commandOutputFirstArchiveRow("t418_command_output_first_archive_mixed_one_sided", "mixed stdout/stderr one-sided"),
		recoveryExpandRow("t419_expand_contentarchive", "contentarchive", "local-archive://<id>", true),
		recoveryExpandRow("t419_expand_toolarchive", "toolarchive", "local-archive://<id>", true),
		recoveryExpandBodyRow(),
		wssCapturedOutputArchiveRow(),
		wssArchiveReinjectRow(),
		httpArchiveRecoveryNoteRow(),
		t417ClassBRecoveryGateRow(),
		t408SidebandReferenceRow(),
		t408BackendReferenceResearchRow(),
	)
	for i := range rows {
		rows[i].ProductReady, rows[i].Blockers = evaluateRecoveryContractRow(rows[i])
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Lane != rows[j].Lane {
			return rows[i].Lane < rows[j].Lane
		}
		return rows[i].ID < rows[j].ID
	})
	report := recoveryContractMatrixReport{
		Rows:             rows,
		Summary:          summarizeRecoveryContractRows(rows),
		HighImpactNext:   recoveryContractHighImpactNext(),
		AcceptancePolicy: recoveryContractAcceptancePolicy(),
	}
	for _, row := range rows {
		if len(row.Blockers) > 0 {
			gap := recoveryContractGapView{ID: row.ID, Blockers: append([]string(nil), row.Blockers...)}
			report.BlockedRows = append(report.BlockedRows, gap)
			if row.DefaultEligible {
				report.ProductGapRows = append(report.ProductGapRows, gap)
			}
		}
	}
	return report
}

func layer0RecoveryContractRows() []recoveryContractRow {
	registry := filter.Layer0ReducerRegistry()
	rows := make([]recoveryContractRow, 0, len(registry))
	for _, reducer := range registry {
		rows = append(rows, recoveryContractRow{
			ID:                  "layer0_" + reducer.ID,
			Lane:                "layer0_parser_default",
			Surface:             "filter.Layer0ReducerRegistry",
			ContentClass:        reducer.Family,
			OmittedBytes:        "parser_bounded_no_archive",
			DefaultEligible:     reducer.DefaultEligible,
			ModelVisible:        true,
			UpstreamVisible:     true,
			DeterministicParser: true,
			FailOpen:            true,
			MechanicalRecovery:  true,
			RecoveryPath:        reducer.RecoveryPath,
			AccountingPath:      "filter_runs positive savings only; no recovery expansion because parser failure full-passes original bytes",
			RequiredFields:      append([]string(nil), reducer.RequiredFields...),
			PreservedEvidence:   append([]string(nil), reducer.PreservedEvidence...),
			FailOpenPredicates:  []string{"parser miss", "panic", "non-positive reducer byte savings", "product default file-read guard"},
			EstimatedImpact:     "already active Layer-0/parser mass; expands T418 safely when command-output-first seam is scoped",
			NextAction:          "Use as parser candidate set; archive-backed contract required only when the compacted class omits evidence bytes beyond preserved fields.",
		})
	}
	return rows
}

func commandOutputFirstArchiveRow(id, stream string) recoveryContractRow {
	return recoveryContractRow{
		ID:                  id,
		Lane:                "t418_command_output_first",
		Surface:             "cmd/slimference/command_output_first.go archiveCommandOutputFirstCompaction",
		ContentClass:        "command stdout/stderr parser-bounded diagnostics and metadata",
		OmittedBytes:        "archive_backed_parser_bounded",
		DefaultEligible:     true,
		ModelVisible:        true,
		UpstreamVisible:     true,
		DeterministicParser: true,
		ArchiveExact:        true,
		MechanicalRecovery:  true,
		NegativeAccounting:  true,
		FailOpen:            true,
		RecoveryPath:        "contentarchive exact raw stream + local-archive URI + slimference expand",
		AccountingPath:      "command-output-first filter_runs positive row; slimference expand records [archive-recovery:contentarchive] negative row",
		RequiredFields:      []string{"command identity", "cwd", "argv", "exit code", stream, "raw bytes", "compacted parser output"},
		PreservedEvidence:   []string{"stream distinction", "parser-preserved diagnostic/metadata facts", "local-archive URI", "recover: slimference expand URI"},
		FailOpenPredicates:  []string{"archive unavailable", "parser miss", "non-positive net after marker", "unsafe command shape", "ambiguous mixed stream"},
		EstimatedImpact:     "+20 to +40 local points on command-output-heavy sessions after high-mass classes are exhausted",
		NextAction:          "Keep adding only high-mass parser classes; use this row as the default acceptance contract.",
	}
}

func recoveryExpandRow(id, kind, uriShape string, negativeAccounting bool) recoveryContractRow {
	return recoveryContractRow{
		ID:                 id,
		Lane:               "t419_mechanical_recovery",
		Surface:            "cmd/slimference/checkpoint_cmd.go handleExpandCmd",
		ContentClass:       kind,
		OmittedBytes:       "recovery_expansion",
		DefaultEligible:    true,
		InternalOnly:       false,
		MechanicalRecovery: true,
		ArchiveExact:       true,
		NegativeAccounting: negativeAccounting,
		FailOpen:           true,
		RecoveryPath:       "slimference expand " + uriShape,
		AccountingPath:     "[archive-recovery:" + kind + "] slimference expand rows in filter_runs",
		RequiredFields:     []string{"archive id", "gzip payload", "metadata JSON", "body bytes"},
		PreservedEvidence:  []string{"exact archived body bytes"},
		FailOpenPredicates: []string{"missing archive", "corrupt gzip", "write error", "empty body no-op accounting"},
		EstimatedImpact:    "enabling contract; recovery cost is subtracted from local savings instead of hidden",
		NextAction:         "Keep as the common recovery primitive for T418/T417/T408 archive-backed omitted bytes.",
	}
}

func recoveryExpandBodyRow() recoveryContractRow {
	return recoveryContractRow{
		ID:                 "t419_expand_body_symbol",
		Lane:               "t419_mechanical_recovery",
		Surface:            "cmd/slimference/checkpoint_cmd.go handleExpandBodyCmd",
		ContentClass:       "Go symbol body recovery",
		OmittedBytes:       "narrow_symbol_recovery",
		DefaultEligible:    false,
		MechanicalRecovery: true,
		ArchiveExact:       true,
		NegativeAccounting: false,
		FailOpen:           true,
		RecoveryPath:       "slimference expand-body <archive-id> <go-symbol>",
		AccountingPath:     "none; diagnostic/operator recovery only",
		RequiredFields:     []string{"archive id", "symbol name", "Go source parser"},
		PreservedEvidence:  []string{"exact requested Go symbol body"},
		FailOpenPredicates: []string{"missing archive", "symbol not found", "parse failure"},
		EstimatedImpact:    "small; useful as recovery affordance, not a default savings unlock",
		NextAction:         "Do not prioritize until major command-output and WSS/server-state lanes are done.",
	}
}

func wssCapturedOutputArchiveRow() recoveryContractRow {
	return recoveryContractRow{
		ID:                  "wss_captured_output_context_archive",
		Lane:                "t417_t419_wss_archive",
		Surface:             "internal/proxy/layer0_proxy.go archiveProxyCapturedOutput",
		ContentClass:        "Codex WSS captured tool output",
		OmittedBytes:        "archive_backed_parser_bounded",
		DefaultEligible:     true,
		ModelVisible:        true,
		UpstreamVisible:     true,
		DeterministicParser: true,
		ArchiveExact:        true,
		MechanicalRecovery:  true,
		NegativeAccounting:  true,
		FailOpen:            true,
		RecoveryPath:        "contentarchive exact payload + [context-archive kind=tool-output uri=local-archive://...] + slimference expand/reinject",
		AccountingPath:      "proxy Layer-0 stats plus slimference expand negative accounting; HTTP/WSS reinject records invalidation/reinject counters",
		RequiredFields:      []string{"session id", "command line", "original payload", "compacted payload", "context archive URI"},
		PreservedEvidence:   []string{"Codex exec envelope header", "parser-preserved tool evidence", "context archive URI"},
		FailOpenPredicates:  []string{"missing session", "archive error", "empty compacted output", "non-positive route economics", "policy guard"},
		EstimatedImpact:     "+10 to +25 local points on WSS tool-output-heavy sessions when paired with T417 state safety",
		NextAction:          "Use only on Class-B/server-state-safe shapes until T417 reroute/detach mechanics are net-positive.",
	}
}

func wssArchiveReinjectRow() recoveryContractRow {
	return recoveryContractRow{
		ID:                      "wss_http_archive_reinject_before_upstream",
		Lane:                    "t419_rehydrate_before_upstream",
		Surface:                 "internal/proxy/reinject.go reinjectArchivedContentForSession",
		ContentClass:            "model-requested local-archive URI",
		OmittedBytes:            "rehydrate_before_dependency",
		DefaultEligible:         true,
		ModelVisible:            true,
		UpstreamVisible:         true,
		RehydrateBeforeUpstream: true,
		ArchiveExact:            true,
		MechanicalRecovery:      true,
		NegativeAccounting:      true,
		FailOpen:                true,
		RecoveryPath:            "scan message text for local-archive URI, session-match, contentarchive.Get, append [reinjected from URI] exact bytes",
		AccountingPath:          "contentarchive re_inject_count/re_inject_bytes_raw/re_inject_tokens_estimate plus qualityNetSavings invalidation; explicit expand path records filter_runs negative cost",
		RequiredFields:          []string{"session id", "archive id", "same-session or sessionless archive metadata", "max reinject budget", "re_inject_bytes_raw", "re_inject_tokens_estimate"},
		PreservedEvidence:       []string{"exact body bytes before downstream decision", "stable URI remains visible on miss", "separate rehydrate byte/token cost counters"},
		FailOpenPredicates:      []string{"missing home", "missing archive", "session mismatch", "max eight reinjects per request", "corrupt payload"},
		EstimatedImpact:         "enables aggressive archive-backed classes by making explicit dependency recovery mechanical",
		NextAction:              "Keep this as mandatory for any model-visible URI dependency before broader WSS omitted-byte activation.",
	}
}

func httpArchiveRecoveryNoteRow() recoveryContractRow {
	return recoveryContractRow{
		ID:                  "http_wss_archive_recovery_note",
		Lane:                "t419_recovery_affordance",
		Surface:             "internal/proxy/handler.go and internal/proxy/wsmitm_phasef.go archiveRecoveryNoteText",
		ContentClass:        "archive/chunk reference affordance",
		OmittedBytes:        "recovery_affordance_text",
		DefaultEligible:     true,
		ModelVisible:        true,
		UpstreamVisible:     true,
		DeterministicParser: true,
		MechanicalRecovery:  true,
		FailOpen:            true,
		RecoveryPath:        "once-per-session note only when archive-backed refs are present or policy mode requires recovery",
		AccountingPath:      "note overhead counted in request tokens; recovery expansion counted by expand/reinject paths",
		RequiredFields:      []string{"session id", "archive-backed stats", "once-per-session reservation"},
		PreservedEvidence:   []string{"explicit URI request instruction", "original user/system content otherwise unchanged"},
		FailOpenPredicates:  []string{"missing session for archive refs reverts mutation", "note injection failure reverts mutation"},
		EstimatedImpact:     "prevents recovery usability drawdown; small overhead, large unlock value for archive-backed reductions",
		NextAction:          "Keep narrow; do not inject when no archive-backed refs exist.",
	}
}

func t417ClassBRecoveryGateRow() recoveryContractRow {
	return recoveryContractRow{
		ID:                  "t417_class_b_server_state_recovery_gate",
		Lane:                "t417_server_state_continuation",
		Surface:             "WSS Class-B/server-state continuation candidate gate",
		ContentClass:        "full-history WSS tool-output mutation",
		OmittedBytes:        "server_state_safe_archive_backed",
		DefaultEligible:     false,
		ModelVisible:        true,
		UpstreamVisible:     true,
		DeterministicParser: true,
		ArchiveExact:        true,
		MechanicalRecovery:  true,
		NegativeAccounting:  true,
		FailOpen:            true,
		RecoveryPath:        "T419 archive/reinject contract plus T417 stateless or lineage-safe reroute before product activation",
		AccountingPath:      "T354/T417 shape proof plus filter/proxy net local savings and recovery cost",
		RequiredFields:      []string{"request class", "previous_response_id/server-state shape", "downstream delta safety", "cache-bust counters", "invalid/400 counters"},
		PreservedEvidence:   []string{"parser-preserved tool evidence", "exact archive URI", "no upstream 400/invalid/cache-bust regression"},
		FailOpenPredicates:  []string{"delta/server-state ambiguity", "recovery loop", "cache-bust", "HTTP 400", "invalid_request", "negative net economics"},
		EstimatedImpact:     "+15 to +30 local points on WSS when the Class-B/server-state knot is solved",
		NextAction:          "Implement only after T419 recovery and T354/T417 shape proof show zero-error net-positive candidate rows.",
	}
}

func t408SidebandReferenceRow() recoveryContractRow {
	return recoveryContractRow{
		ID:                      "t408_sideband_rehydrate_bridge",
		Lane:                    "t408_reference_lane",
		Surface:                 "local sideband reference bridge",
		ContentClass:            "local archive reference before upstream",
		OmittedBytes:            "local_reference_rehydrate_before_upstream",
		DefaultEligible:         true,
		InternalOnly:            true,
		RehydrateBeforeUpstream: true,
		ArchiveExact:            true,
		MechanicalRecovery:      true,
		NegativeAccounting:      true,
		FailOpen:                true,
		RecoveryPath:            "local reference may exist only inside Slimference; exact bytes are rehydrated before upstream-visible payload",
		AccountingPath:          "rehydrated bytes count as recovery/invalidation cost; upstream sees no local-only reference",
		RequiredFields:          []string{"reference id", "exact archive entry", "rehydration point", "upstream no-local-reference invariant"},
		PreservedEvidence:       []string{"exact original bytes upstream-visible before dependency"},
		FailOpenPredicates:      []string{"missing archive", "ambiguous route", "would leak local-only URI upstream", "oversized recovery set"},
		EstimatedImpact:         "+10 to +30 local points only if it composes with T417/T408 productization; otherwise enabling value",
		NextAction:              "Use as the policy-safe Lane 2; never treat local IDs as backend memory.",
	}
}

func t408BackendReferenceResearchRow() recoveryContractRow {
	return recoveryContractRow{
		ID:                 "t408_backend_honored_reference_lane3",
		Lane:               "t408_reference_lane",
		Surface:            "backend-honored reference research",
		ContentClass:       "server-held content reference",
		OmittedBytes:       "backend_reference_research",
		DefaultEligible:    false,
		InternalOnly:       false,
		MechanicalRecovery: false,
		FailOpen:           true,
		RecoveryPath:       "probe-only until a backend accepts a content reference and returns byte-equivalent state behavior",
		AccountingPath:     "none until a real accepted reference contract exists",
		RequiredFields:     []string{"accepted backend reference field", "response id lineage", "byte-equivalent downstream behavior", "rejection fallback"},
		PreservedEvidence:  []string{"none in product path yet"},
		FailOpenPredicates: []string{"unknown field ignored", "invalid_request", "state mismatch", "response id mismatch", "reference not honored"},
		EstimatedImpact:    "+10 to +30 local points if a real backend reference contract exists; 0 on current known product path",
		NextAction:         "Probe as research only; productize narrowly if and only if the backend actually honors the reference contract.",
	}
}

func evaluateRecoveryContractRow(row recoveryContractRow) (bool, []string) {
	var blockers []string
	if !row.DefaultEligible {
		blockers = append(blockers, "not_default_eligible")
	}
	if row.ModelVisible && row.InternalOnly {
		blockers = append(blockers, "model_visible_marked_internal_only")
	}
	if row.OmittedBytes != "parser_bounded_no_archive" && row.OmittedBytes != "recovery_affordance_text" && row.OmittedBytes != "narrow_symbol_recovery" {
		if !row.ArchiveExact {
			blockers = append(blockers, "missing_exact_archive")
		}
		if !row.MechanicalRecovery {
			blockers = append(blockers, "missing_mechanical_recovery")
		}
	}
	if recoveryContractRequiresNegativeAccounting(row.OmittedBytes) {
		if row.DefaultEligible && !row.NegativeAccounting && row.OmittedBytes != "recovery_affordance_text" {
			blockers = append(blockers, "missing_negative_accounting")
		}
	}
	if row.OmittedBytes != "recovery_expansion" && row.OmittedBytes != "narrow_symbol_recovery" && !row.FailOpen {
		blockers = append(blockers, "missing_fail_open")
	}
	if row.DeterministicParser && len(row.PreservedEvidence) == 0 {
		blockers = append(blockers, "missing_preserved_evidence")
	}
	if row.RehydrateBeforeUpstream && !row.ArchiveExact {
		blockers = append(blockers, "rehydrate_without_exact_archive")
	}
	return len(blockers) == 0, blockers
}

func recoveryContractRequiresNegativeAccounting(omittedBytes string) bool {
	switch omittedBytes {
	case "archive_backed_parser_bounded",
		"server_state_safe_archive_backed",
		"local_reference_rehydrate_before_upstream",
		"rehydrate_before_dependency",
		"recovery_expansion":
		return true
	default:
		return false
	}
}

func summarizeRecoveryContractRows(rows []recoveryContractRow) recoveryContractSummary {
	var summary recoveryContractSummary
	summary.Rows = len(rows)
	for _, row := range rows {
		if row.ProductReady {
			summary.ProductReady++
		}
		if len(row.Blockers) > 0 {
			summary.BlockedRows++
			if row.DefaultEligible {
				summary.ProductGaps++
			}
		}
		if row.DefaultEligible {
			summary.DefaultEligibleRows++
		} else if len(row.Blockers) > 0 {
			summary.NonDefaultBlockedRows++
		}
		if row.ArchiveExact {
			summary.ArchiveBackedRows++
		}
		if row.RehydrateBeforeUpstream {
			summary.RehydrateBeforeRows++
		}
		switch row.Lane {
		case "layer0_parser_default":
			summary.Layer0RegistryRows++
		case "t418_command_output_first":
			summary.CommandOutputRows++
		case "t417_server_state_continuation":
			summary.ServerStateLaneRows++
		case "t419_mechanical_recovery":
			summary.ManualRecoveryRows++
		case "t408_reference_lane":
			summary.ResearchRows++
		}
	}
	return summary
}

func recoveryContractHighImpactNext() []string {
	return []string{
		"T418: keep adding only high-mass command-output-first parser classes that satisfy archive-before-replace, fail-open, marker-inclusive positive economics, and recovery negative accounting.",
		"T419: use this matrix as a gate before any bytes-omitting T417/T408/T418 product activation.",
		"T420: if live Desktop reconnect handoff mass appears, fix transport/lifecycle first because byte-transparent mass elimination beats reducer micro-work.",
		"T417: unlock Class-B/server-state continuation only with zero-error net-positive shape proof plus T419 recovery for omitted bytes.",
		"T408: run Lane 1/2 probes without leaking local-only references upstream; Lane 3 is product only after the backend accepts a real reference contract.",
	}
}

func recoveryContractAcceptancePolicy() []string {
	return []string{
		"Parser-bounded reducers may omit bytes only when preserved evidence is explicit and parser miss/panic/non-positive output full-passes original bytes.",
		"Archive-backed reducers must archive exact raw bytes before replacement and expose a mechanical recovery path.",
		"Recovery expansion or reinjection cost must be counted as negative local savings or equivalent invalidation cost.",
		"Local-only archive/reference IDs must not be treated as backend memory and must be rehydrated before upstream if downstream state depends on omitted bytes.",
		"Any HTTP 400, invalid_request, cache-bust, lost frame, recovery loop, or model-quality regression blocks product activation for that exact shape.",
	}
}

func writeRecoveryContractMatrixText(w io.Writer, report recoveryContractMatrixReport) {
	fmt.Fprintln(w, "T419 recovery-contract matrix")
	fmt.Fprintf(w, "Rows: %d, product-ready: %d, product gaps: %d, archive-backed: %d, rehydrate-before-upstream: %d\n",
		report.Summary.Rows,
		report.Summary.ProductReady,
		report.Summary.ProductGaps,
		report.Summary.ArchiveBackedRows,
		report.Summary.RehydrateBeforeRows,
	)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Product gaps:")
	if len(report.ProductGapRows) == 0 {
		fmt.Fprintln(w, "  none")
	} else {
		for _, gap := range report.ProductGapRows {
			fmt.Fprintf(w, "  %s: %s\n", gap.ID, strings.Join(gap.Blockers, ", "))
		}
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "High-impact next:")
	for _, item := range report.HighImpactNext {
		fmt.Fprintf(w, "  - %s\n", item)
	}
}
