package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/control"
)

type codexDesktopStatusFlags struct {
	host string
	port string
	json bool
	help bool
}

type codexDesktopProveFlags struct {
	host       string
	port       string
	duration   time.Duration
	capture    string
	matrixPath string
	json       bool
	keepOpen   bool
	replace    bool
	reuse      bool
	manual     bool
	finish     bool
	help       bool
}

type codexDesktopStatusOutput struct {
	Mode                        string                   `json:"mode"`
	FailureClass                string                   `json:"failure_class,omitempty"`
	ProxyURL                    string                   `json:"proxy_url"`
	CATrust                     codexDesktopCAState      `json:"ca_trust"`
	DaemonReachable             bool                     `json:"daemon_reachable"`
	DaemonError                 string                   `json:"daemon_error,omitempty"`
	WSS                         control.WSSState         `json:"wss"`
	WSSCountersScope            string                   `json:"wss_counters_scope"`
	AppServerSupportsWebSockets bool                     `json:"app_server_supports_websockets"`
	AppServerAutoMode           string                   `json:"app_server_auto_mode"`
	AppServerAutoReason         string                   `json:"app_server_auto_reason,omitempty"`
	LiveProofRequired           bool                     `json:"live_proof_required"`
	ConversationObserved        bool                     `json:"conversation_observed"`
	LaunchCommand               string                   `json:"launch_command"`
	ProofStartedAt              string                   `json:"proof_started_at,omitempty"`
	CapturePath                 string                   `json:"capture_path,omitempty"`
	MatrixPath                  string                   `json:"matrix_path,omitempty"`
	SearchCapProofCommand       string                   `json:"search_cap_proof_command,omitempty"`
	MatrixRowCommand            string                   `json:"matrix_row_command,omitempty"`
	FocusedMatrixCommand        string                   `json:"focused_matrix_command,omitempty"`
	WSSSocketsCommand           string                   `json:"wss_sockets_command,omitempty"`
	WSSAuditCommand             string                   `json:"wss_audit_command,omitempty"`
	WSSProofPackCommand         string                   `json:"wss_proof_pack_command,omitempty"`
	ManualProofCommand          string                   `json:"manual_proof_command,omitempty"`
	OwnerPrompt                 string                   `json:"owner_prompt,omitempty"`
	FinishCommand               string                   `json:"finish_command,omitempty"`
	ClassDistributionCommand    string                   `json:"class_distribution_command,omitempty"`
	NextSteps                   []string                 `json:"next_steps,omitempty"`
	LastProof                   *codexDesktopProofOutput `json:"last_proof,omitempty"`
	Notes                       []string                 `json:"notes,omitempty"`
}

type codexDesktopProofOutput struct {
	Mode                     string              `json:"mode"`
	FailureClass             string              `json:"failure_class,omitempty"`
	Transport                string              `json:"transport,omitempty"`
	Duration                 string              `json:"duration"`
	StartedAt                string              `json:"started_at,omitempty"`
	LaunchPID                int                 `json:"launch_pid,omitempty"`
	LaunchOutput             string              `json:"launch_output,omitempty"`
	CapturePath              string              `json:"capture_path,omitempty"`
	MatrixPath               string              `json:"matrix_path,omitempty"`
	SearchCapProofCommand    string              `json:"search_cap_proof_command,omitempty"`
	MatrixRowCommand         string              `json:"matrix_row_command,omitempty"`
	FocusedMatrixCommand     string              `json:"focused_matrix_command,omitempty"`
	WSSSocketsCommand        string              `json:"wss_sockets_command,omitempty"`
	WSSAuditCommand          string              `json:"wss_audit_command,omitempty"`
	WSSProofPackCommand      string              `json:"wss_proof_pack_command,omitempty"`
	DeltaWSS                 control.WSSState    `json:"delta_wss"`
	CATrust                  codexDesktopCAState `json:"ca_trust"`
	SessionPath              string              `json:"session_path,omitempty"`
	ClassDistributionCommand string              `json:"class_distribution_command,omitempty"`
	CleanupAttempted         bool                `json:"cleanup_attempted"`
	CleanupError             string              `json:"cleanup_error,omitempty"`
	LaunchReady              bool                `json:"launch_ready"`
	DesktopProven            bool                `json:"desktop_proven"`
	DesktopSavings           bool                `json:"desktop_savings"`
	ManualPromptStill        bool                `json:"manual_prompt_still_required"`
	Notes                    []string            `json:"notes,omitempty"`
}

type codexDesktopProofSession struct {
	SchemaVersion int              `json:"schema_version"`
	Host          string           `json:"host"`
	Port          string           `json:"port"`
	Transport     string           `json:"transport,omitempty"`
	LaunchPID     int              `json:"launch_pid"`
	StartedAt     time.Time        `json:"started_at"`
	BaselineWSS   control.WSSState `json:"baseline_wss"`
	LaunchOutput  string           `json:"launch_output,omitempty"`
	CapturePath   string           `json:"capture_path,omitempty"`
	MatrixPath    string           `json:"matrix_path,omitempty"`
}

const codexDesktopProofStateTimeout = 10 * time.Second

const codexDesktopOwnerProofPrompt = "In the current Slimference repository, run a longer real search-loop coding-session proof workload: read AGENTS.md, docs/todo.md, inspect internal/proxy/wsmitm_phasef.go and internal/filter/builtin_testrun.go, run multiple rg searches with enough matching output, follow with sed/git/go test commands, and analyze WSS savings blockers without editing files. End with PROOF_DONE."

const codexDesktopManualProofCommand = "slimference codex desktop prove --manual --json --duration=30s --keep-open"

const codexDesktopReuseProofCommand = "slimference codex desktop prove --manual --reuse-running --json --duration=5s --keep-open"

const codexDesktopFinishProofCommand = "slimference codex desktop prove --finish --json"

const codexDesktopProofSinceFilePath = "/tmp/slimference-desktop-proof-since.txt"

const codexDesktopClassDistributionCommand = "go run ./scripts/utils wss-class-distribution ~/.slimference/debug/decisions.jsonl --since-file=" + codexDesktopProofSinceFilePath + " --min-local-ratio=0.48 --json"

const codexDesktopClassDistributionCommandPrefix = "go run ./scripts/utils wss-class-distribution ~/.slimference/debug/decisions.jsonl"

var codexDesktopProofSinceFilePathFn = func() string { return codexDesktopProofSinceFilePath }

var codexDesktopProofCapturePathFn = codexDesktopDefaultProofCapturePath

func formatCodexDesktopProofStartedAt(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func codexDesktopDefaultProofCapturePath(startedAt time.Time) string {
	if startedAt.IsZero() {
		startedAt = codexNowFn().UTC()
	}
	stamp := startedAt.UTC().Format("20060102T150405Z")
	home, err := osUserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return filepath.Join(os.TempDir(), "slimference-captures", "codex-desktop-proof-"+stamp, "frames.jsonl")
	}
	return filepath.Join(home, ".slimference", "captures", "codex-desktop-proof-"+stamp, "frames.jsonl")
}

func expandCodexDesktopProofPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", nil
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := osUserHomeDir()
		if err != nil || strings.TrimSpace(home) == "" {
			return "", fmt.Errorf("home unresolved for %s", path)
		}
		if path == "~" {
			return home, nil
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~/")), nil
	}
	return path, nil
}

func codexDesktopProofMatrixPath(capturePath string) string {
	capturePath = strings.TrimSpace(capturePath)
	if capturePath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(capturePath), "matrix.jsonl")
}

func prepareCodexDesktopProofCapturePath(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create Desktop proof capture dir: %w", err)
	}
	return nil
}

func applyCodexDesktopProofCaptureCommands(out *codexDesktopProofOutput, host string, port string) {
	if out == nil || strings.TrimSpace(out.CapturePath) == "" {
		return
	}
	socketSeq := codexDesktopProofSocketSeq(out)
	out.SearchCapProofCommand = codexDesktopSearchCapProofCommand(out.CapturePath, socketSeq)
	if strings.TrimSpace(out.MatrixPath) == "" {
		out.MatrixPath = codexDesktopProofMatrixPath(out.CapturePath)
	}
	if strings.TrimSpace(out.MatrixPath) == "" {
		return
	}
	out.MatrixRowCommand = codexDesktopMatrixRowCommand(out.MatrixPath, out.CapturePath, host, port, socketSeq)
	out.FocusedMatrixCommand = codexDesktopFocusedMatrixCommand(out.MatrixPath)
}

func applyCodexDesktopProofPackCommands(out *codexDesktopProofOutput) {
	if out == nil {
		return
	}
	out.WSSSocketsCommand, out.WSSAuditCommand, out.WSSProofPackCommand = codexDesktopProofPackCommands(out.StartedAt)
}

func codexDesktopSearchCapProofCommand(capturePath string, socketSeq uint64) string {
	return "go run ./scripts/utils search-cap-proof --frames " + capturePath + codexDesktopSocketSeqFlag(socketSeq) + " --candidate=30:15 --candidate=25:15 --min-candidate-retained-pct=40 --min-search-outputs=2 --min-extra-reducer-tokens=1 --json"
}

func codexDesktopMatrixRowCommand(matrixPath string, capturePath string, host string, port string, socketSeq uint64) string {
	host = firstNonEmpty(strings.TrimSpace(host), "127.0.0.1")
	port = firstNonEmpty(strings.TrimSpace(port), "8990")
	return "go run ./scripts/utils wss-proof-live-row --matrix-row " + matrixPath + " --frames " + capturePath + codexDesktopSocketSeqFlag(socketSeq) + " --client desktop --workload-class search_loop --expected-reducer captured_output --host " + host + " --port " + port + " --json"
}

func codexDesktopFocusedMatrixCommand(matrixPath string) string {
	return "go run ./scripts/utils wss-proof-matrix " + matrixPath + " --require-live-token-delta --required-workload=search_loop --min-captures=1 --min-desktop=1 --min-positive=1 --expected-reducer captured_output --search-cap-candidate=30:15 --search-cap-candidate=25:15 --search-cap-min-retained-pct=40 --search-cap-min-search-outputs=2 --search-cap-min-extra-tokens=1 --json"
}

func codexDesktopClassDistributionCommandForSince(startedAt string) string {
	startedAt = normalizeCodexDesktopProofStartedAt(startedAt)
	if startedAt == "" {
		return codexDesktopClassDistributionCommand
	}
	return codexDesktopClassDistributionCommandPrefix + " --since=" + startedAt + " --min-local-ratio=0.48 --json"
}

func codexDesktopProofPackCommands(startedAt string) (string, string, string) {
	sinceFlag := "--since-file=" + codexDesktopProofSinceFilePathFn()
	stamp := "since-file"
	if normalized := normalizeCodexDesktopProofStartedAt(startedAt); normalized != "" {
		sinceFlag = "--since=" + normalized
		stamp = codexDesktopProofArtifactStamp(normalized)
	}
	socketsPath := "/tmp/slimference-desktop-proof-wss-sockets-" + stamp + ".json"
	auditPath := "/tmp/slimference-desktop-proof-wss-audit-" + stamp + ".json"
	sockets := "slimference debug wss-sockets 200 " + sinceFlag + " --json > " + socketsPath
	audit := "go run ./scripts/utils wss-audit ~/.slimference/debug/decisions.jsonl " + sinceFlag + " --json > " + auditPath
	pack := "go run ./scripts/utils wss-proof-pack ~/.slimference/debug/decisions.jsonl " + sinceFlag + " --sockets-json=" + socketsPath + " --audit-json=" + auditPath + " --json"
	return sockets, audit, pack
}

func codexDesktopProofArtifactStamp(startedAt string) string {
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(startedAt))
	if err != nil {
		return "since-file"
	}
	return t.UTC().Format("20060102T150405Z")
}

func normalizeCodexDesktopProofStartedAt(startedAt string) string {
	startedAt = strings.TrimSpace(startedAt)
	if startedAt == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, startedAt)
	if err != nil {
		return ""
	}
	return formatCodexDesktopProofStartedAt(t)
}

func codexDesktopProofSocketSeq(out *codexDesktopProofOutput) uint64 {
	if out == nil {
		return 0
	}
	if socketSeq := singleCodexDesktopProofSocketSeq(out.DeltaWSS.RecentSockets, 0); socketSeq != 0 {
		return socketSeq
	}
	startedAt := normalizeCodexDesktopProofStartedAt(out.StartedAt)
	if startedAt == "" {
		return 0
	}
	t, err := time.Parse(time.RFC3339, startedAt)
	if err != nil {
		return 0
	}
	return singleCodexDesktopProofSocketSeq(out.DeltaWSS.RecentSockets, t.UnixNano())
}

func singleCodexDesktopProofSocketSeq(sockets []control.WSSSocketLifecycle, minOpenedAtUnixNano int64) uint64 {
	var socketSeq uint64
	for _, socket := range sockets {
		if socket.SocketSeq == 0 {
			continue
		}
		if minOpenedAtUnixNano > 0 && socket.OpenedAtUnixNano > 0 && socket.OpenedAtUnixNano < minOpenedAtUnixNano {
			continue
		}
		if socket.C2SFrames == 0 && socket.S2CFrames == 0 && socket.TurnsCompleted == 0 {
			continue
		}
		if socketSeq != 0 && socketSeq != socket.SocketSeq {
			return 0
		}
		socketSeq = socket.SocketSeq
	}
	return socketSeq
}

func codexDesktopSocketSeqFlag(socketSeq uint64) string {
	if socketSeq == 0 {
		return ""
	}
	return " --socket-seq=" + strconv.FormatUint(socketSeq, 10)
}

func readCodexDesktopLegacyProofSinceFileStartedAt() string {
	path := strings.TrimSpace(codexDesktopProofSinceFilePathFn())
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return normalizeCodexDesktopProofStartedAt(string(data))
}

func chooseCodexDesktopProofStartedAt(primary string, legacy string) string {
	primary = normalizeCodexDesktopProofStartedAt(primary)
	legacy = normalizeCodexDesktopProofStartedAt(legacy)
	if primary == "" {
		return legacy
	}
	if legacy == "" {
		return primary
	}
	primaryTime, primaryErr := time.Parse(time.RFC3339, primary)
	legacyTime, legacyErr := time.Parse(time.RFC3339, legacy)
	if primaryErr != nil || legacyErr != nil {
		return primary
	}
	if legacyTime.Before(primaryTime) && primaryTime.Sub(legacyTime) <= 10*time.Minute {
		return legacy
	}
	return primary
}

func runCodexDesktopCmd(args []string, p installPrinter) int {
	if len(args) == 0 {
		fmt.Fprint(p.Out, codexDesktopHelpText)
		return 0
	}
	switch args[0] {
	case "status":
		return runCodexDesktopStatusCmd(args[1:], p)
	case "prove":
		return runCodexDesktopProveCmd(args[1:], p)
	case "--help", "-h", "help":
		fmt.Fprint(p.Out, codexDesktopHelpText)
		return 0
	default:
		fmt.Fprintf(p.Err, "codex desktop: unknown subcommand %q\n", args[0])
		fmt.Fprint(p.Err, codexDesktopUsageLine)
		return 2
	}
}

func runCodexDesktopStatusCmd(args []string, p installPrinter) int {
	flags, err := parseCodexDesktopStatusFlags(args)
	if err != nil {
		fmt.Fprintf(p.Err, "codex desktop status: %v\n", err)
		return 2
	}
	if flags.help {
		fmt.Fprint(p.Out, codexDesktopStatusHelpText)
		return 0
	}
	out := buildCodexDesktopStatus(flags)
	if flags.json {
		enc := json.NewEncoder(p.Out)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
		return 0
	}
	renderCodexDesktopStatus(p.Out, out)
	return 0
}

func runCodexDesktopProveCmd(args []string, p installPrinter) int {
	flags, err := parseCodexDesktopProveFlags(args)
	if err != nil {
		fmt.Fprintf(p.Err, "codex desktop prove: %v\n", err)
		return 2
	}
	if flags.help {
		fmt.Fprint(p.Out, codexDesktopProveHelpText)
		return 0
	}
	if flags.finish {
		return runCodexDesktopFinishProof(flags, p)
	}

	startedAt := codexNowFn().UTC()
	startedAtText := formatCodexDesktopProofStartedAt(startedAt)
	capturePath := strings.TrimSpace(flags.capture)
	if capturePath == "" && flags.manual {
		capturePath = codexDesktopProofCapturePathFn(startedAt)
	}
	if capturePath != "" {
		var expandErr error
		capturePath, expandErr = expandCodexDesktopProofPath(capturePath)
		if expandErr != nil {
			out := codexDesktopProofOutput{
				Mode:         "capture_path_invalid",
				FailureClass: "capture_path_invalid",
				Duration:     flags.duration.String(),
				StartedAt:    startedAtText,
				Notes:        []string{expandErr.Error()},
			}
			applyCodexDesktopProofPackCommands(&out)
			emitCodexDesktopProof(p, flags.json, out)
			return 2
		}
	}
	matrixPath := strings.TrimSpace(flags.matrixPath)
	if matrixPath != "" {
		var expandErr error
		matrixPath, expandErr = expandCodexDesktopProofPath(matrixPath)
		if expandErr != nil {
			out := codexDesktopProofOutput{
				Mode:         "matrix_path_invalid",
				FailureClass: "matrix_path_invalid",
				Duration:     flags.duration.String(),
				StartedAt:    startedAtText,
				CapturePath:  capturePath,
				Notes:        []string{expandErr.Error()},
			}
			applyCodexDesktopProofPackCommands(&out)
			emitCodexDesktopProof(p, flags.json, out)
			return 2
		}
	}
	if matrixPath == "" {
		matrixPath = codexDesktopProofMatrixPath(capturePath)
	}
	before, err := codexSetupStateFn(flags.host, flags.port, 2*time.Second)
	if err != nil {
		out := codexDesktopProofOutput{
			Mode:         "daemon_unreachable",
			FailureClass: "daemon_unreachable",
			Duration:     flags.duration.String(),
			StartedAt:    startedAtText,
			CapturePath:  capturePath,
			MatrixPath:   matrixPath,
			Notes:        []string{"start the Slimference daemon before running the Desktop proof"},
		}
		applyCodexDesktopProofCaptureCommands(&out, flags.host, flags.port)
		applyCodexDesktopProofPackCommands(&out)
		emitCodexDesktopProof(p, flags.json, out)
		return 1
	}
	if err := prepareCodexDesktopProofCapturePath(capturePath); err != nil {
		out := codexDesktopProofOutput{
			Mode:         "capture_prepare_failed",
			FailureClass: "capture_prepare_failed",
			Duration:     flags.duration.String(),
			StartedAt:    startedAtText,
			CapturePath:  capturePath,
			MatrixPath:   matrixPath,
			Notes:        []string{err.Error()},
		}
		applyCodexDesktopProofCaptureCommands(&out, flags.host, flags.port)
		applyCodexDesktopProofPackCommands(&out)
		emitCodexDesktopProof(p, flags.json, out)
		return 1
	}
	daemonCaptureArmed := false
	if capturePath != "" {
		if err := codexDesktopWSSCaptureFn(flags.host, flags.port, capturePath, true, 2*time.Second); err != nil {
			out := codexDesktopProofOutput{
				Mode:         "capture_enable_failed",
				FailureClass: "capture_enable_failed",
				Duration:     flags.duration.String(),
				StartedAt:    startedAtText,
				CapturePath:  capturePath,
				MatrixPath:   matrixPath,
				Notes:        []string{"daemon-side Desktop proof WSS capture could not be enabled: " + err.Error()},
			}
			applyCodexDesktopProofCaptureCommands(&out, flags.host, flags.port)
			applyCodexDesktopProofPackCommands(&out)
			emitCodexDesktopProof(p, flags.json, out)
			return 1
		}
		daemonCaptureArmed = true
	}

	var launchOut, launchErr strings.Builder
	launchPID := 0
	rc := 0
	if flags.reuse {
		var reuseErr error
		launchPID, reuseErr = reusableCodexDesktopProofPID()
		if reuseErr != nil {
			out := codexDesktopProofOutput{
				Mode:         "reuse_running_unavailable",
				FailureClass: "reuse_running_unavailable",
				Duration:     flags.duration.String(),
				StartedAt:    startedAtText,
				Transport:    codexDesktopTransportAppServer,
				CapturePath:  capturePath,
				MatrixPath:   matrixPath,
				Notes:        []string{reuseErr.Error()},
			}
			applyCodexDesktopProofCaptureCommands(&out, flags.host, flags.port)
			applyCodexDesktopProofPackCommands(&out)
			if daemonCaptureArmed {
				clearCodexDesktopDaemonCapture(&out, flags.host, flags.port)
			}
			emitCodexDesktopProof(p, flags.json, out)
			return 1
		}
		launchOut.WriteString(fmt.Sprintf("Codex.app reused (PID %d) from previous scoped Desktop proof.", launchPID))
	} else {
		launchArgs := []string{"--transport=app-server", "--host=" + flags.host, "--port=" + flags.port}
		if flags.replace {
			launchArgs = append(launchArgs, "--replace-existing")
		}
		rc = runCodexLaunchDesktopCmd(launchArgs, installPrinter{Out: &launchOut, Err: &launchErr})
		launchPID = parseCodexDesktopLaunchPID(launchOut.String())
	}
	out := codexDesktopProofOutput{
		Duration:          flags.duration.String(),
		StartedAt:         startedAtText,
		Transport:         codexDesktopTransportAppServer,
		LaunchOutput:      strings.TrimSpace(launchOut.String()),
		CapturePath:       capturePath,
		MatrixPath:        matrixPath,
		ManualPromptStill: true,
		Notes: []string{
			"automated proof covers launch-time app-server shim routing",
			"proof arms daemon-side WSS A/B capture when capture_path is set and a prompt is run",
			"full Desktop savings proof still needs a prompt-tied WSS delta if launch-time bytes do not flow",
		},
	}
	if flags.reuse {
		out.Notes = append(out.Notes, "reused the previous scoped Desktop proof app; no existing Codex.app session was replaced")
	}
	applyCodexDesktopProofCaptureCommands(&out, flags.host, flags.port)
	applyCodexDesktopProofPackCommands(&out)
	out.LaunchPID = launchPID
	if rc != 0 {
		out.Mode = "launch_failed"
		out.FailureClass = "launch_failed"
		msg := strings.TrimSpace(launchErr.String())
		if msg == "" {
			msg = out.LaunchOutput
		}
		if strings.Contains(msg, "Codex.app is already running") {
			out.FailureClass = "codex_desktop_already_running"
			out.Notes = append(out.Notes, "quit Codex.app yourself, or rerun with --replace-existing when interrupting the current Desktop session is intentional")
		}
		if msg != "" {
			out.Notes = append(out.Notes, msg)
		}
		if daemonCaptureArmed {
			clearCodexDesktopDaemonCapture(&out, flags.host, flags.port)
		}
		emitCodexDesktopProof(p, flags.json, out)
		return 1
	}

	time.Sleep(flags.duration)
	after, err := codexSetupStateFn(flags.host, flags.port, codexDesktopProofStateTimeout)
	if err != nil {
		out.Mode = "post_probe_failed"
		out.FailureClass = "post_probe_failed"
		out.Notes = append(out.Notes, err.Error())
		cleanupCodexDesktopProof(&out, false)
		if daemonCaptureArmed {
			clearCodexDesktopDaemonCapture(&out, flags.host, flags.port)
		}
		emitCodexDesktopProof(p, flags.json, out)
		return 1
	}
	out.DeltaWSS = codexSetupDelta(before, after).WSS
	classifyCodexDesktopProof(&out, flags.manual)
	if flags.manual && (out.LaunchReady || out.DesktopSavings) {
		out.ClassDistributionCommand = codexDesktopClassDistributionCommandForSince(out.StartedAt)
		applyCodexDesktopProofPackCommands(&out)
		if err := writeCodexDesktopProofSession(flags, before.WSS, startedAt, &out); err != nil {
			out.Mode = "session_write_failed"
			out.FailureClass = "session_write_failed"
			out.LaunchReady = false
			out.DesktopSavings = false
			out.DesktopProven = false
			out.ManualPromptStill = false
			out.Notes = append(out.Notes, err.Error())
		}
	}
	keepOpen := flags.keepOpen || (flags.manual && (out.LaunchReady || out.DesktopSavings))
	cleanupCodexDesktopProof(&out, keepOpen)
	if daemonCaptureArmed && !(flags.manual && (out.LaunchReady || out.DesktopSavings)) {
		clearCodexDesktopDaemonCapture(&out, flags.host, flags.port)
	}
	writeCodexDesktopProofResult(&out)
	emitCodexDesktopProof(p, flags.json, out)
	if out.DesktopSavings || (flags.manual && out.LaunchReady) {
		return 0
	}
	return 1
}

func runCodexDesktopFinishProof(flags codexDesktopProveFlags, p installPrinter) int {
	sessionPath := codexDesktopSessionFn()
	session, err := readCodexDesktopProofSession(sessionPath)
	if err != nil {
		out := codexDesktopProofOutput{
			Mode:         "session_unavailable",
			FailureClass: "session_unavailable",
			Duration:     "0s",
			SessionPath:  sessionPath,
			Notes:        []string{err.Error()},
		}
		emitCodexDesktopProof(p, flags.json, out)
		return 1
	}
	after, err := codexSetupStateFn(session.Host, session.Port, codexDesktopProofStateTimeout)
	if err != nil {
		startedAtText := formatCodexDesktopProofStartedAt(session.StartedAt)
		out := codexDesktopProofOutput{
			Mode:                     "daemon_unreachable",
			FailureClass:             "daemon_unreachable",
			Duration:                 time.Since(session.StartedAt).Round(time.Second).String(),
			StartedAt:                startedAtText,
			LaunchPID:                session.LaunchPID,
			SessionPath:              sessionPath,
			CapturePath:              session.CapturePath,
			MatrixPath:               session.MatrixPath,
			ClassDistributionCommand: codexDesktopClassDistributionCommandForSince(startedAtText),
			Notes:                    []string{err.Error()},
		}
		applyCodexDesktopProofCaptureCommands(&out, session.Host, session.Port)
		applyCodexDesktopProofPackCommands(&out)
		if session.CapturePath != "" {
			clearCodexDesktopDaemonCapture(&out, session.Host, session.Port)
		}
		emitCodexDesktopProof(p, flags.json, out)
		return 1
	}
	before := control.SetupState{WSS: session.BaselineWSS}
	startedAtText := formatCodexDesktopProofStartedAt(session.StartedAt)
	out := codexDesktopProofOutput{
		Duration:                 time.Since(session.StartedAt).Round(time.Second).String(),
		StartedAt:                startedAtText,
		Transport:                firstNonEmpty(session.Transport, codexDesktopTransportAppServer),
		LaunchPID:                session.LaunchPID,
		LaunchOutput:             session.LaunchOutput,
		SessionPath:              sessionPath,
		CapturePath:              session.CapturePath,
		MatrixPath:               session.MatrixPath,
		ClassDistributionCommand: codexDesktopClassDistributionCommandForSince(startedAtText),
		DeltaWSS:                 codexSetupDelta(before, after).WSS,
		Notes:                    []string{"finish compares current daemon WSS state to the manual Desktop proof baseline"},
	}
	applyCodexDesktopProofCaptureCommands(&out, session.Host, session.Port)
	applyCodexDesktopProofPackCommands(&out)
	classifyCodexDesktopProof(&out, false)
	if session.CapturePath != "" {
		clearCodexDesktopDaemonCapture(&out, session.Host, session.Port)
	}
	writeCodexDesktopProofResult(&out)
	emitCodexDesktopProof(p, flags.json, out)
	if out.DesktopSavings {
		return 0
	}
	return 1
}

func parseCodexDesktopProveFlags(args []string) (codexDesktopProveFlags, error) {
	f := codexDesktopProveFlags{host: "127.0.0.1", port: "8990", duration: 15 * time.Second}
	for _, a := range args {
		switch {
		case a == "--help" || a == "-h":
			f.help = true
		case a == "--json":
			f.json = true
		case a == "--keep-open":
			f.keepOpen = true
		case a == "--replace-existing":
			f.replace = true
		case a == "--reuse-running":
			f.reuse = true
			f.keepOpen = true
		case a == "--manual":
			f.manual = true
			f.keepOpen = true
		case a == "--finish":
			f.finish = true
		case strings.HasPrefix(a, "--host="):
			f.host = strings.TrimPrefix(a, "--host=")
		case strings.HasPrefix(a, "--port="):
			f.port = strings.TrimPrefix(a, "--port=")
		case strings.HasPrefix(a, "--capture="):
			f.capture = strings.TrimSpace(strings.TrimPrefix(a, "--capture="))
			if f.capture == "" {
				return f, fmt.Errorf("--capture requires a non-empty path")
			}
		case strings.HasPrefix(a, "--matrix-row="):
			f.matrixPath = strings.TrimSpace(strings.TrimPrefix(a, "--matrix-row="))
			if f.matrixPath == "" {
				return f, fmt.Errorf("--matrix-row requires a non-empty path")
			}
		case strings.HasPrefix(a, "--duration="):
			d, err := time.ParseDuration(strings.TrimPrefix(a, "--duration="))
			if err != nil || d <= 0 {
				return f, fmt.Errorf("--duration must be a positive Go duration")
			}
			f.duration = d
		default:
			return f, fmt.Errorf("unknown flag %q", a)
		}
	}
	if f.manual && f.finish {
		return f, fmt.Errorf("--manual and --finish cannot be combined")
	}
	if f.reuse && !f.manual {
		return f, fmt.Errorf("--reuse-running requires --manual")
	}
	if f.reuse && f.replace {
		return f, fmt.Errorf("--reuse-running and --replace-existing cannot be combined")
	}
	return f, nil
}

func parseCodexDesktopStatusFlags(args []string) (codexDesktopStatusFlags, error) {
	f := codexDesktopStatusFlags{host: "127.0.0.1", port: "8990"}
	for _, a := range args {
		switch {
		case a == "--help" || a == "-h":
			f.help = true
		case a == "--json":
			f.json = true
		case strings.HasPrefix(a, "--host="):
			f.host = strings.TrimPrefix(a, "--host=")
		case strings.HasPrefix(a, "--port="):
			f.port = strings.TrimPrefix(a, "--port=")
		default:
			return f, fmt.Errorf("unknown flag %q", a)
		}
	}
	return f, nil
}

func buildCodexDesktopStatus(flags codexDesktopStatusFlags) codexDesktopStatusOutput {
	proxyURL := fmt.Sprintf("http://%s:%s", flags.host, flags.port)
	appRoute := resolveCodexDesktopAppServerRoute()
	runningPIDs, runningErr := currentCodexDesktopPIDs()
	out := codexDesktopStatusOutput{
		Mode:                        "not_ready",
		ProxyURL:                    proxyURL,
		CATrust:                     codexDesktopCATrustFn(),
		WSSCountersScope:            "daemon_cumulative_not_desktop_proof",
		AppServerSupportsWebSockets: appRoute.SupportsWebSockets,
		AppServerAutoMode:           appRoute.Mode,
		AppServerAutoReason:         appRoute.Reason,
		LiveProofRequired:           true,
		LaunchCommand:               "slimference codex launch-desktop --transport=app-server",
	}
	if !appRoute.SupportsWebSockets {
		out.Notes = append(out.Notes, "next Desktop app-server launch uses HTTP savings fallback until WSS Phase-F is freshly certified")
	}
	if runningErr != nil {
		out.Notes = append(out.Notes, "Codex.app running-state probe failed: "+runningErr.Error())
	}
	if last, err := readCodexDesktopProofResult(codexDesktopResultFn()); err == nil {
		applyCodexDesktopProofCaptureCommands(last, flags.host, flags.port)
		applyCodexDesktopProofPackCommands(last)
		out.LastProof = last
	}
	state, err := codexSetupStateFn(flags.host, flags.port, 2*time.Second)
	if err != nil {
		out.FailureClass = "daemon_unreachable"
		out.DaemonError = err.Error()
		out.Notes = append(out.Notes, "start the Slimference daemon before launching Codex Desktop through the app-server shim")
		return out
	}
	out.DaemonReachable = true
	out.WSS = state.WSS
	if out.CATrust.Exists && !out.CATrust.Trusted {
		out.Notes = append(out.Notes, "Keychain trust is not required for the preferred app-server shim route")
	}
	if codexDesktopHasWSSActivity(state.WSS) {
		out.Notes = append(out.Notes, "WSS counters are daemon-wide and may include Codex CLI traffic; Desktop proof requires a pre/post delta tied to the spawned Codex.app process")
	}
	if len(runningPIDs) > 0 && !codexDesktopLastProofOwnsRunningApp(out.LastProof, runningPIDs) {
		out.Mode = "codex_desktop_already_running"
		out.FailureClass = "codex_desktop_already_running"
		out.LiveProofRequired = true
		out.ConversationObserved = false
		out.Notes = append(out.Notes, "Codex.app is already running (PID "+joinDesktopPIDs(runningPIDs)+"); quit it first so scoped Slimference env can be injected, or pass --replace-existing only when interrupting that session is intentional")
		applyCodexDesktopOwnerSessionHandoff(&out, false)
		return out
	}
	if out.LastProof != nil && out.LastProof.Mode == "desktop_ready_for_prompt" &&
		!codexDesktopLastProofOwnsRunningApp(out.LastProof, runningPIDs) {
		out.Notes = append(out.Notes, "last Desktop prompt handoff is stale because the scoped Codex.app launch PID is no longer running; start a new manual Desktop proof before pasting the owner prompt")
	} else if out.LastProof != nil {
		applyCodexDesktopLastProof(&out, out.LastProof, codexDesktopReusableProofAppActive(out.LastProof, runningPIDs))
		if out.Mode != "not_ready" {
			return out
		}
	}
	if !out.CATrust.Exists {
		out.Notes = append(out.Notes, "local CA is absent; not required for app-server shim route")
	}
	out.Mode = "ready_for_live_desktop_probe"
	out.Notes = append(out.Notes, "launch Codex Desktop through the app-server shim and verify .wss deltas in /_slimference/admin/state")
	return out
}

func currentCodexDesktopPIDs() ([]int, error) {
	appPath := strings.TrimSpace(codexDesktopAppPathFn())
	if appPath == "" {
		return nil, nil
	}
	binary := filepath.Join(appPath, defaultCodexDesktopExecRelPath)
	if _, err := codexDesktopStatFn(binary); err != nil {
		return nil, nil
	}
	return codexDesktopRunningFn(binary)
}

func codexDesktopLastProofOwnsRunningApp(last *codexDesktopProofOutput, runningPIDs []int) bool {
	if last == nil || last.LaunchPID <= 0 {
		return false
	}
	for _, pid := range runningPIDs {
		if pid == last.LaunchPID {
			return true
		}
	}
	return false
}

func codexDesktopReusableProofAppActive(last *codexDesktopProofOutput, runningPIDs []int) bool {
	return codexDesktopLastProofOwnsRunningApp(last, runningPIDs) && codexDesktopAppServerActiveFn()
}

func reusableCodexDesktopProofPID() (int, error) {
	last, err := readCodexDesktopProofResult(codexDesktopResultFn())
	if err != nil {
		return 0, fmt.Errorf("no previous Desktop proof result to reuse: %w", err)
	}
	if last.Transport != codexDesktopTransportAppServer {
		return 0, fmt.Errorf("previous Desktop proof transport %q is not reusable app-server", last.Transport)
	}
	runningPIDs, err := currentCodexDesktopPIDs()
	if err != nil {
		return 0, fmt.Errorf("Codex.app running-state probe failed: %w", err)
	}
	if !codexDesktopLastProofOwnsRunningApp(last, runningPIDs) {
		return 0, fmt.Errorf("running Codex.app is not owned by the previous scoped Desktop proof")
	}
	if !codexDesktopAppServerActiveFn() {
		return 0, fmt.Errorf("previous scoped Desktop proof app is running but no app-server shim process is active")
	}
	return last.LaunchPID, nil
}

func applyCodexDesktopLastProof(out *codexDesktopStatusOutput, last *codexDesktopProofOutput, reusable bool) {
	if last.Transport != "" && last.Transport != codexDesktopTransportAppServer {
		out.Notes = append(out.Notes, "last Desktop proof used legacy "+last.Transport+" route; app-server shim proof is still required")
		return
	}
	if last.Transport == "" && strings.HasPrefix(last.Mode, "desktop_proxy") {
		out.Notes = append(out.Notes, "last Desktop proof predates app-server shim route; app-server proof is still required")
		return
	}
	switch last.Mode {
	case "desktop_app_server_phasef_proven":
		out.Mode = "desktop_app_server_proven"
		out.FailureClass = ""
		out.ConversationObserved = true
		out.LiveProofRequired = false
		out.Notes = append(out.Notes, "last Desktop app-server shim route proof was green; state-safe WSS status compaction is allowed, broader WSS tool-output mutation remains lab/proof opt-in")
	case "desktop_app_server_route_proven":
		// Launch-eligible, but NOT a savings claim: the conversation reached the
		// Phase-F route with zero errors, yet this proof saw no mutation. Kept as
		// a distinct status so the TUI never sells "route ready" as "savings
		// proven"; a green savings claim requires desktop_app_server_phasef_proven.
		out.Mode = "desktop_app_server_route_ready"
		out.FailureClass = ""
		out.ConversationObserved = true
		out.LiveProofRequired = false
		out.Notes = append(out.Notes, "last Desktop app-server proof reached the Phase-F WSS route with zero errors; launch is allowed, but per-turn savings (mutation) were not proven in that proof and scale with conversation size like the CLI")
	case "desktop_ready_for_prompt":
		out.Mode = "desktop_proof_prompt_required"
		out.FailureClass = "prompt_required"
		applyCodexDesktopPromptRequiredHandoff(out, reusable)
	case "desktop_ca_env_rejected":
		out.Mode = "desktop_direct_only"
		out.FailureClass = firstNonEmpty(last.FailureClass, "tls_trust_rejected")
		out.Notes = append(out.Notes, "last Desktop proof reached Slimference CONNECT but closed before application bytes; use normal Codex.app direct launch")
	case "desktop_no_wss_delta":
		out.Mode = "desktop_direct_only"
		out.FailureClass = firstNonEmpty(last.FailureClass, "no_wss_delta")
		out.Notes = append(out.Notes, "last Desktop proof produced no Desktop WSS delta; use normal Codex.app direct launch")
		applyCodexDesktopOwnerSessionHandoff(out, reusable)
	case "desktop_app_server_wss_bridge":
		out.Mode = "desktop_wss_bridge_only"
		out.FailureClass = "desktop_savings_not_proven"
		out.ConversationObserved = true
		out.Notes = append(out.Notes, "last Desktop proof carried WSS bytes but did not prove Phase-F savings")
	}
}

func applyCodexDesktopOwnerSessionHandoff(out *codexDesktopStatusOutput, reusable bool) {
	if reusable {
		out.ManualProofCommand = codexDesktopReuseProofCommand
	} else {
		out.ManualProofCommand = codexDesktopManualProofCommand
	}
	out.OwnerPrompt = codexDesktopOwnerProofPrompt
	out.FinishCommand = codexDesktopFinishProofCommand
	if reusable {
		out.NextSteps = append(out.NextSteps,
			"Run manual_proof_command to arm a fresh capture on the existing scoped Codex.app proof window; do not replace Codex.app.",
			"Paste owner_prompt only into that existing scoped Codex.app window.",
			"Run finish_command after the prompt completes; then run the class_distribution_command printed by the finish result.",
		)
	} else {
		out.NextSteps = append(out.NextSteps,
			"Quit the current Codex.app yourself, or use --replace-existing only when interrupting that Desktop session is intentional.",
			"Run manual_proof_command to launch a scoped Codex.app with Slimference env.",
			"Paste owner_prompt only into that newly launched scoped Codex.app window.",
			"Run finish_command after the prompt completes; then run the class_distribution_command printed by the finish result.",
		)
	}
}

func applyCodexDesktopPromptRequiredHandoff(out *codexDesktopStatusOutput, reusable bool) {
	startedAt := codexDesktopPromptProofStartedAt(out.LastProof)
	out.ProofStartedAt = startedAt
	if reusable {
		out.ManualProofCommand = codexDesktopReuseProofCommand
	} else {
		out.ManualProofCommand = codexDesktopManualProofCommand
	}
	out.OwnerPrompt = codexDesktopOwnerProofPrompt
	out.FinishCommand = codexDesktopFinishProofCommand
	out.ClassDistributionCommand = codexDesktopClassDistributionCommandForSince(startedAt)
	out.WSSSocketsCommand, out.WSSAuditCommand, out.WSSProofPackCommand = codexDesktopProofPackCommands(startedAt)
	applyCodexDesktopStatusCaptureHandoff(out)
	if reusable {
		out.NextSteps = append(out.NextSteps,
			"Paste owner_prompt into the existing scoped Codex.app proof window, or run manual_proof_command first to arm a fresh capture on it.",
			"Run finish_command after the prompt completes.",
			"Run matrix_row_command, focused_matrix_command, search_cap_proof_command, and class_distribution_command; continue guard work only when all focused proof gates pass and headroom_present=true.",
		)
	} else {
		out.NextSteps = append(out.NextSteps,
			"Paste owner_prompt into the scoped Codex.app window that was launched by the manual Desktop proof.",
			"Run finish_command after the prompt completes.",
			"Run matrix_row_command, focused_matrix_command, search_cap_proof_command, and class_distribution_command; continue guard work only when all focused proof gates pass and headroom_present=true.",
		)
	}
	if reusable {
		out.NextSteps = append(out.NextSteps, "For a fresh capture path on the same scoped proof app, manual_proof_command already uses --reuse-running instead of replacing Codex.app.")
	} else if out.LastProof != nil && out.LastProof.LaunchPID > 0 {
		out.NextSteps = append(out.NextSteps, "For a fresh capture path on the same scoped proof app, run `"+codexDesktopReuseProofCommand+"` instead of replacing Codex.app.")
	}
	out.Notes = append(out.Notes, "last Desktop proof launched successfully but still needs a prompt plus `"+codexDesktopFinishProofCommand+"`")
}

func applyCodexDesktopStatusCaptureHandoff(out *codexDesktopStatusOutput) {
	if out == nil {
		return
	}
	var capturePath string
	var matrixPath string
	host := "127.0.0.1"
	port := "8990"
	if out.LastProof != nil {
		capturePath = out.LastProof.CapturePath
		matrixPath = out.LastProof.MatrixPath
	}
	if session, err := readCodexDesktopProofSession(codexDesktopSessionFn()); err == nil {
		sessionMatchesLastProof := out.LastProof == nil || out.LastProof.LaunchPID <= 0 || session.LaunchPID == out.LastProof.LaunchPID
		if sessionMatchesLastProof {
			host = firstNonEmpty(session.Host, host)
			port = firstNonEmpty(session.Port, port)
			if strings.TrimSpace(capturePath) == "" {
				capturePath = session.CapturePath
			}
			matrixPath = firstNonEmpty(matrixPath, session.MatrixPath)
		}
	}
	capturePath = strings.TrimSpace(capturePath)
	if capturePath == "" {
		return
	}
	out.CapturePath = capturePath
	out.MatrixPath = firstNonEmpty(strings.TrimSpace(matrixPath), codexDesktopProofMatrixPath(capturePath))
	proof := &codexDesktopProofOutput{CapturePath: out.CapturePath, MatrixPath: out.MatrixPath}
	applyCodexDesktopProofCaptureCommands(proof, host, port)
	out.SearchCapProofCommand = proof.SearchCapProofCommand
	out.MatrixRowCommand = proof.MatrixRowCommand
	out.FocusedMatrixCommand = proof.FocusedMatrixCommand
}

func codexDesktopPromptProofStartedAt(last *codexDesktopProofOutput) string {
	if last != nil {
		if startedAt := normalizeCodexDesktopProofStartedAt(last.StartedAt); startedAt != "" {
			return startedAt
		}
	}
	session, err := readCodexDesktopProofSession(codexDesktopSessionFn())
	if err != nil {
		return readCodexDesktopLegacyProofSinceFileStartedAt()
	}
	if last != nil && last.LaunchPID > 0 && session.LaunchPID != last.LaunchPID {
		return ""
	}
	return chooseCodexDesktopProofStartedAt(
		formatCodexDesktopProofStartedAt(session.StartedAt),
		readCodexDesktopLegacyProofSinceFileStartedAt(),
	)
}

func classifyCodexDesktopProof(out *codexDesktopProofOutput, manual bool) {
	w := out.DeltaWSS
	switch {
	case w.PhasefBridged > 0 && w.ParseFailures == 0 && w.DegradedSessions == 0 && w.CompressionErrors == 0:
		// A real Codex WSS conversation reached the Phase-F savings route
		// (FrameBridge ran) with no parser/degrade/compression errors. This is
		// the reliable, lag-free signal: phasefBridged increments once at upgrade
		// time. Full mutation also requires compressible content in the window;
		// without it the route is still proven and launch-eligible.
		if w.FramesReencoded > 0 && w.CompressedMessagesMutated > 0 {
			out.Mode = "desktop_app_server_phasef_proven"
			out.LaunchReady = true
			out.DesktopProven = true
			out.DesktopSavings = true
			out.ManualPromptStill = false
		} else {
			out.Mode = "desktop_app_server_route_proven"
			out.LaunchReady = true
			out.DesktopProven = true
			out.Notes = append(out.Notes,
				"Desktop conversation reached the Phase-F WSS savings route with zero parser/degrade/compression errors",
				"per-turn token savings scale with conversation size, exactly like the certified CLI path",
			)
		}
	case codexDesktopTLSRejected(w):
		if out.Transport == codexDesktopTransportProxy {
			out.Mode = "desktop_ca_env_rejected"
			out.FailureClass = "tls_trust_rejected"
			out.Notes = append(out.Notes,
				"Codex.app opened CONNECT/WSS bridge sessions but closed before application bytes",
				"process-local CA env reached the app but current Desktop did not use it for this TLS path",
			)
			break
		}
		out.Mode = "desktop_connect_only_no_app_server_bytes"
		out.FailureClass = "connect_only_no_app_server_bytes"
		out.Notes = append(out.Notes,
			"CONNECT-only WSS activity is not proof for the app-server shim route",
			"Desktop savings require bytes, frames, and Phase-F mutation on the app-server local WSS path",
		)
	case w.BytesC2S > 0 && w.BytesS2C > 0 && w.FramesReencoded > 0 && w.CompressedMessagesMutated > 0 &&
		w.ParseFailures == 0 && w.DegradedSessions == 0 && w.CompressionErrors == 0:
		out.Mode = "desktop_app_server_phasef_proven"
		out.LaunchReady = true
		out.DesktopProven = true
		out.DesktopSavings = true
		out.ManualPromptStill = false
	case w.BytesC2S > 0 && w.BytesS2C > 0 &&
		w.ParseFailures == 0 && w.DegradedSessions == 0 && w.CompressionErrors == 0:
		out.Mode = "desktop_app_server_wss_bridge"
		out.LaunchReady = true
		out.DesktopProven = true
		out.Notes = append(out.Notes, "WSS bytes flowed, but Phase-F mutation did not fire in this observation window")
	case codexDesktopHasWSSActivity(w):
		out.Mode = "desktop_wss_needs_review"
		out.FailureClass = "wss_needs_review"
		out.Notes = append(out.Notes, "WSS counters moved, but not enough for a Desktop savings claim")
	case manual:
		out.Mode = "desktop_ready_for_prompt"
		out.LaunchReady = true
		out.ManualPromptStill = true
		out.Notes = append(out.Notes,
			"Codex.app stayed open with scoped Slimference env; send one prompt in that app, then run `slimference codex desktop prove --finish --json`",
			"Desktop savings are not claimed until the finish step sees bytes, frames, and Phase-F mutation",
		)
	default:
		out.Mode = "desktop_no_wss_delta"
		out.FailureClass = "no_wss_delta"
		out.Notes = append(out.Notes, "no daemon WSS delta appeared during the automated observation window")
	}
}

func cleanupCodexDesktopProof(out *codexDesktopProofOutput, keepOpen bool) {
	if keepOpen || out.LaunchPID <= 0 {
		return
	}
	out.CleanupAttempted = true
	if err := codexDesktopCleanupFn(out.LaunchPID); err != nil {
		out.CleanupError = err.Error()
	}
}

func clearCodexDesktopDaemonCapture(out *codexDesktopProofOutput, host string, port string) {
	if err := codexDesktopWSSCaptureFn(host, port, "", false, 2*time.Second); err != nil {
		out.Notes = append(out.Notes, "daemon-side WSS capture disarm failed: "+err.Error())
		return
	}
	out.Notes = append(out.Notes, "daemon-side WSS capture disarmed")
}

func cleanupCodexDesktopProcess(pid int) error {
	if pid <= 0 {
		return nil
	}
	if err := signalCodexDesktopProcess(pid, syscall.SIGTERM); err != nil {
		return err
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !codexDesktopProcessAlive(pid) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return signalCodexDesktopProcess(pid, syscall.SIGKILL)
}

func signalCodexDesktopProcess(pid int, sig syscall.Signal) error {
	if err := syscall.Kill(-pid, sig); err != nil && err != syscall.ESRCH {
		if procErr := syscall.Kill(pid, sig); procErr != nil && procErr != syscall.ESRCH {
			return procErr
		}
	}
	return nil
}

func codexDesktopProcessAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

func codexDesktopProofSessionPath() string {
	home, err := osUserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return filepath.Join(os.TempDir(), "slimference-codex-desktop-proof.json")
	}
	return filepath.Join(home, ".slimference", "codex-desktop-proof.json")
}

func codexDesktopProofResultPath() string {
	home, err := osUserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return filepath.Join(os.TempDir(), "slimference-codex-desktop-proof-result.json")
	}
	return filepath.Join(home, ".slimference", "codex-desktop-proof-result.json")
}

func writeCodexDesktopProofResult(out *codexDesktopProofOutput) {
	path := codexDesktopResultFn()
	if path == "" {
		return
	}
	out.SessionPath = firstNonEmpty(out.SessionPath, codexDesktopSessionFn())
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		out.Notes = append(out.Notes, "failed to encode Desktop proof result: "+err.Error())
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		out.Notes = append(out.Notes, "failed to create Desktop proof result dir: "+err.Error())
		return
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		out.Notes = append(out.Notes, "failed to write Desktop proof result: "+err.Error())
	}
}

func readCodexDesktopProofResult(path string) (*codexDesktopProofOutput, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out codexDesktopProofOutput
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func writeCodexDesktopProofSession(flags codexDesktopProveFlags, baseline control.WSSState, startedAt time.Time, out *codexDesktopProofOutput) error {
	path := codexDesktopSessionFn()
	if startedAt.IsZero() {
		startedAt = codexNowFn().UTC()
	}
	startedAtText := formatCodexDesktopProofStartedAt(startedAt)
	session := codexDesktopProofSession{
		SchemaVersion: 1,
		Host:          flags.host,
		Port:          flags.port,
		Transport:     out.Transport,
		LaunchPID:     out.LaunchPID,
		StartedAt:     startedAt.UTC(),
		BaselineWSS:   baseline,
		LaunchOutput:  out.LaunchOutput,
		CapturePath:   out.CapturePath,
		MatrixPath:    out.MatrixPath,
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create Desktop proof session dir: %w", err)
	}
	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Desktop proof session: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write Desktop proof session: %w", err)
	}
	out.SessionPath = path
	out.StartedAt = startedAtText
	out.ClassDistributionCommand = codexDesktopClassDistributionCommandForSince(startedAtText)
	return nil
}

func readCodexDesktopProofSession(path string) (codexDesktopProofSession, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return codexDesktopProofSession{}, fmt.Errorf("read Desktop proof session %s: %w", path, err)
	}
	var session codexDesktopProofSession
	if err := json.Unmarshal(data, &session); err != nil {
		return codexDesktopProofSession{}, fmt.Errorf("decode Desktop proof session %s: %w", path, err)
	}
	if session.SchemaVersion != 1 {
		return codexDesktopProofSession{}, fmt.Errorf("unsupported Desktop proof session schema %d", session.SchemaVersion)
	}
	if session.Host == "" || session.Port == "" {
		return codexDesktopProofSession{}, fmt.Errorf("Desktop proof session missing host or port")
	}
	return session, nil
}

func emitCodexDesktopProof(p installPrinter, asJSON bool, out codexDesktopProofOutput) {
	if asJSON {
		enc := json.NewEncoder(p.Out)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
		return
	}
	renderCodexDesktopProof(p.Out, out)
}

func renderCodexDesktopProof(w io.Writer, out codexDesktopProofOutput) {
	fmt.Fprintln(w, "Slimference Codex Desktop Proof")
	fmt.Fprintln(w, "-------------------------------")
	fmt.Fprintf(w, "  Mode      %s\n", out.Mode)
	if out.FailureClass != "" {
		fmt.Fprintf(w, "  Gate      %s\n", out.FailureClass)
	}
	fmt.Fprintf(w, "  Duration  %s\n", out.Duration)
	if out.StartedAt != "" {
		fmt.Fprintf(w, "  Started   %s\n", out.StartedAt)
	}
	if out.LaunchPID > 0 {
		fmt.Fprintf(w, "  PID       %d\n", out.LaunchPID)
	}
	if out.Transport != "" {
		fmt.Fprintf(w, "  Transport %s\n", out.Transport)
	}
	if out.SessionPath != "" {
		fmt.Fprintf(w, "  Session   %s\n", out.SessionPath)
	}
	if out.CapturePath != "" {
		fmt.Fprintf(w, "  Capture   %s\n", out.CapturePath)
	}
	if out.ClassDistributionCommand != "" {
		fmt.Fprintf(w, "  Measure   %s\n", out.ClassDistributionCommand)
	}
	if out.MatrixRowCommand != "" {
		fmt.Fprintf(w, "  Row       %s\n", out.MatrixRowCommand)
	}
	if out.FocusedMatrixCommand != "" {
		fmt.Fprintf(w, "  Matrix    %s\n", out.FocusedMatrixCommand)
	}
	if out.SearchCapProofCommand != "" {
		fmt.Fprintf(w, "  SearchCap %s\n", out.SearchCapProofCommand)
	}
	if out.WSSSocketsCommand != "" {
		fmt.Fprintf(w, "  Sockets   %s\n", out.WSSSocketsCommand)
	}
	if out.WSSAuditCommand != "" {
		fmt.Fprintf(w, "  Audit     %s\n", out.WSSAuditCommand)
	}
	if out.WSSProofPackCommand != "" {
		fmt.Fprintf(w, "  ProofPack %s\n", out.WSSProofPackCommand)
	}
	fmt.Fprintf(w, "  Delta WSS mitm=%d bytes_c2s=%d bytes_s2c=%d frames_reencoded=%d inspected=%d mutated=%d parse_failures=%d degraded=%d compression_errors=%d\n",
		out.DeltaWSS.MITMBridged, out.DeltaWSS.BytesC2S, out.DeltaWSS.BytesS2C,
		out.DeltaWSS.FramesReencoded, out.DeltaWSS.CompressedMessagesInspected,
		out.DeltaWSS.CompressedMessagesMutated, out.DeltaWSS.ParseFailures,
		out.DeltaWSS.DegradedSessions, out.DeltaWSS.CompressionErrors)
	fmt.Fprintf(w, "  Proven    launch_ready=%v desktop=%v savings=%v manual_prompt_required=%v\n", out.LaunchReady, out.DesktopProven, out.DesktopSavings, out.ManualPromptStill)
	fmt.Fprintf(w, "  Cleanup   attempted=%v\n", out.CleanupAttempted)
	if out.CleanupError != "" {
		fmt.Fprintf(w, "            %s\n", out.CleanupError)
	}
	for _, note := range out.Notes {
		fmt.Fprintf(w, "  Note      %s\n", note)
	}
}

func renderCodexDesktopStatus(w io.Writer, out codexDesktopStatusOutput) {
	fmt.Fprintln(w, "Slimference Codex Desktop")
	fmt.Fprintln(w, "-------------------------")
	fmt.Fprintf(w, "  Mode      %s\n", out.Mode)
	if out.FailureClass != "" {
		fmt.Fprintf(w, "  Gate      %s\n", out.FailureClass)
	}
	fmt.Fprintf(w, "  Proxy     %s\n", out.ProxyURL)
	fmt.Fprintf(w, "  CA        exists=%v trusted=%v\n", out.CATrust.Exists, out.CATrust.Trusted)
	if out.CATrust.Path != "" {
		fmt.Fprintf(w, "            %s\n", out.CATrust.Path)
	}
	if out.CATrust.Error != "" {
		fmt.Fprintf(w, "            %s\n", out.CATrust.Error)
	}
	fmt.Fprintf(w, "  Daemon    reachable=%v\n", out.DaemonReachable)
	if out.DaemonError != "" {
		fmt.Fprintf(w, "            %s\n", out.DaemonError)
	}
	fmt.Fprintf(w, "  WSS       mitm=%d inspected=%d mutated=%d parse_failures=%d degraded=%d compression_errors=%d\n",
		out.WSS.MITMBridged, out.WSS.CompressedMessagesInspected, out.WSS.CompressedMessagesMutated,
		out.WSS.ParseFailures, out.WSS.DegradedSessions, out.WSS.CompressionErrors)
	fmt.Fprintf(w, "            scope=%s\n", out.WSSCountersScope)
	fmt.Fprintf(w, "  AppSrv    supports_websockets=%v auto=%s\n", out.AppServerSupportsWebSockets, out.AppServerAutoMode)
	if out.AppServerAutoReason != "" {
		fmt.Fprintf(w, "            %s\n", out.AppServerAutoReason)
	}
	fmt.Fprintf(w, "  Proof     live_required=%v conversation_observed=%v\n", out.LiveProofRequired, out.ConversationObserved)
	if out.ProofStartedAt != "" {
		fmt.Fprintf(w, "  Since     %s\n", out.ProofStartedAt)
	}
	fmt.Fprintf(w, "  Launch    %s\n", out.LaunchCommand)
	if out.ManualProofCommand != "" {
		fmt.Fprintf(w, "  Manual    %s\n", out.ManualProofCommand)
	}
	if out.CapturePath != "" {
		fmt.Fprintf(w, "  Capture   %s\n", out.CapturePath)
	}
	for _, step := range out.NextSteps {
		fmt.Fprintf(w, "  Next      %s\n", step)
	}
	if out.FinishCommand != "" {
		fmt.Fprintf(w, "  Finish    %s\n", out.FinishCommand)
	}
	if out.ClassDistributionCommand != "" {
		fmt.Fprintf(w, "  Measure   %s\n", out.ClassDistributionCommand)
	}
	if out.MatrixRowCommand != "" {
		fmt.Fprintf(w, "  Row       %s\n", out.MatrixRowCommand)
	}
	if out.FocusedMatrixCommand != "" {
		fmt.Fprintf(w, "  Matrix    %s\n", out.FocusedMatrixCommand)
	}
	if out.SearchCapProofCommand != "" {
		fmt.Fprintf(w, "  SearchCap %s\n", out.SearchCapProofCommand)
	}
	if out.WSSSocketsCommand != "" {
		fmt.Fprintf(w, "  Sockets   %s\n", out.WSSSocketsCommand)
	}
	if out.WSSAuditCommand != "" {
		fmt.Fprintf(w, "  Audit     %s\n", out.WSSAuditCommand)
	}
	if out.WSSProofPackCommand != "" {
		fmt.Fprintf(w, "  ProofPack %s\n", out.WSSProofPackCommand)
	}
	if out.OwnerPrompt != "" {
		fmt.Fprintf(w, "  Prompt    %s\n", out.OwnerPrompt)
	}
	for _, note := range out.Notes {
		fmt.Fprintf(w, "  Note      %s\n", note)
	}
}

func parseCodexDesktopLaunchPID(text string) int {
	var pid int
	if _, err := fmt.Sscanf(text, "Codex.app launched (PID %d)", &pid); err == nil {
		return pid
	}
	return 0
}

func codexDesktopHasWSSActivity(w control.WSSState) bool {
	return w.MITMBridged != 0 ||
		w.PassthroughBridged != 0 ||
		w.BytesC2S != 0 ||
		w.BytesS2C != 0 ||
		w.C2SFrames != 0 ||
		w.S2CFrames != 0 ||
		w.FramesForwarded != 0 ||
		w.FramesReencoded != 0 ||
		w.CompressedMessagesInspected != 0 ||
		w.CompressedMessagesMutated != 0 ||
		w.CompressedMessagesBypassed != 0 ||
		w.PhaseFRequests != 0 ||
		w.PhaseFMutations != 0 ||
		w.ParseFailures != 0 ||
		w.DegradedSessions != 0 ||
		w.CompressionErrors != 0 ||
		w.UpstreamDialFail != 0 ||
		w.Rejected != 0
}

func codexDesktopTLSRejected(w control.WSSState) bool {
	return w.MITMBridged > 0 &&
		w.PhasefBridged == 0 &&
		w.BytesC2S == 0 &&
		w.BytesS2C == 0 &&
		w.C2SFrames == 0 &&
		w.S2CFrames == 0 &&
		w.FramesReencoded == 0 &&
		w.FramesForwarded == 0 &&
		w.CompressedMessagesInspected == 0 &&
		w.CompressedMessagesMutated == 0 &&
		w.PhaseFMutations == 0 &&
		w.UpstreamDialFail == 0 &&
		w.ParseFailures == 0 &&
		w.DegradedSessions == 0 &&
		w.CompressionErrors == 0
}
