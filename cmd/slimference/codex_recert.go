package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/codexroute"
	"github.com/Christopher-Schulze/Slimference/internal/control"
)

const codexRecertLogMaxBytes int64 = 2 << 20

var errCodexRecertLocked = errors.New("codex wss recert already running")

var recertRunCommandFn = runRecertCommand

type codexRecertifyFlags struct {
	subject  string
	host     string
	port     string
	operator string
	notes    string
	timeout  time.Duration
	dryRun   bool
	noWrite  bool
	force    bool
	json     bool
	help     bool
}

type codexRecertTriggerInput struct {
	Host    string
	Port    string
	Timeout time.Duration
}

type codexRecertTriggerResult struct {
	PromptSequence []string `json:"prompt_sequence"`
	Output         string   `json:"output,omitempty"`
}

type codexCriterionOutput struct {
	Name string `json:"name"`
	Got  string `json:"got"`
	Want string `json:"want"`
}

type codexRecertifyResult struct {
	AttemptID          string                   `json:"attempt_id"`
	CodexVersion       string                   `json:"codex_version"`
	SlimferenceVersion string                   `json:"slimference_version"`
	PhaseFPassed       bool                     `json:"phasef_passed"`
	BridgePassed       bool                     `json:"bridge_passed"`
	PhaseFFailures     []codexCriterionOutput   `json:"phasef_failures,omitempty"`
	BridgeFailures     []codexCriterionOutput   `json:"bridge_failures,omitempty"`
	CertificationPath  string                   `json:"certification_path"`
	BridgeProofPath    string                   `json:"bridge_proof_path"`
	RecertStatePath    string                   `json:"recert_state_path"`
	Trigger            codexRecertTriggerResult `json:"trigger"`
	DeltaWSS           control.WSSState         `json:"delta_wss"`
}

func runCodexRecertifyCmd(args []string, p installPrinter) int {
	flags, err := parseCodexRecertifyFlags(args)
	if err != nil {
		fmt.Fprintf(p.Err, "codex recertify: %v\n", err)
		return 2
	}
	if flags.help {
		fmt.Fprint(p.Out, codexRecertifyHelpText)
		return 0
	}
	if flags.subject != "wss" {
		fmt.Fprintln(p.Err, "codex recertify: subject must be wss")
		return 2
	}
	home, err := codexRouteHomeFn()
	if err != nil || home == "" {
		fmt.Fprintln(p.Err, "codex recertify: HOME unresolved")
		return 1
	}
	versionOut, err := codexVersionOutFn()
	if err != nil {
		fmt.Fprintf(p.Err, "codex recertify: codex --version failed: %v\n", err)
		return 1
	}
	codexVersion, err := parseCodexCLIVersion(versionOut)
	if err != nil {
		fmt.Fprintf(p.Err, "codex recertify: %v\n", err)
		return 1
	}
	attemptID := codexNowFn().UTC().Format("20060102T150405.000000000Z")
	result := codexRecertifyResult{
		AttemptID:          attemptID,
		CodexVersion:       codexVersion,
		SlimferenceVersion: version,
		CertificationPath:  codexroute.CertificationPath(home),
		BridgeProofPath:    codexroute.BridgeProofPath(home),
		RecertStatePath:    codexroute.RecertStatePath(home),
	}
	if flags.dryRun {
		return emitCodexRecertifyResult(p.Out, result, flags.json)
	}
	if blocked, reason := codexRecertBackoffActive(home, flags.force); blocked {
		fmt.Fprintf(p.Err, "codex recertify: %s\n", reason)
		return 1
	}
	unlock, err := acquireCodexRecertLock(home)
	if err != nil {
		fmt.Fprintf(p.Err, "codex recertify: %v\n", err)
		return 1
	}
	defer unlock()
	started := codexNowFn().UTC()
	_ = codexRecertSaveFn(home, codexroute.RecertState{
		SchemaVersion:      codexroute.RecertSchemaVersion,
		Status:             "running",
		AttemptID:          attemptID,
		CodexVersion:       codexVersion,
		SlimferenceVersion: version,
		StartedAt:          started,
	})
	codexRecertLogFn(home, "start attempt="+attemptID+" codex="+codexVersion+" slimference="+version)
	pre, err := codexSetupStateFn(flags.host, flags.port, 2*time.Second)
	if err != nil {
		return finishCodexRecertError(p, flags, home, attemptID, codexVersion, "preflight admin state unavailable: "+err.Error())
	}
	trigger, err := codexRecertTriggerFn(codexRecertTriggerInput{Host: flags.host, Port: flags.port, Timeout: flags.timeout})
	result.Trigger = trigger
	if err != nil {
		return finishCodexRecertError(p, flags, home, attemptID, codexVersion, "trigger failed: "+err.Error())
	}
	post, err := codexSetupStateFn(flags.host, flags.port, 2*time.Second)
	if err != nil {
		return finishCodexRecertError(p, flags, home, attemptID, codexVersion, "postflight admin state unavailable: "+err.Error())
	}
	delta := codexSetupDelta(pre, post)
	result.DeltaWSS = delta.WSS
	result.PhaseFFailures = codexCriterionOutputs(codexWSSCertificationFailures(delta))
	result.BridgeFailures = codexCriterionOutputs(codexWSSBridgeFailures(delta))
	result.PhaseFPassed = len(result.PhaseFFailures) == 0
	result.BridgePassed = len(result.BridgeFailures) == 0
	if result.BridgePassed && !flags.noWrite {
		if err := codexBridgeSaveFn(home, codexBridgeProofFromState(delta, codexVersion, flags)); err != nil {
			return finishCodexRecertError(p, flags, home, attemptID, codexVersion, "write bridge proof: "+err.Error())
		}
	}
	if result.PhaseFPassed && !flags.noWrite {
		if err := codexCertSaveFn(home, codexCertFromState(delta, codexVersion, flags)); err != nil {
			return finishCodexRecertError(p, flags, home, attemptID, codexVersion, "write certification: "+err.Error())
		}
	}
	status := "failed"
	lastErr := "wss phase-f proof failed"
	if result.BridgePassed {
		status = "bridge"
		lastErr = "phase-f proof failed; bridge proof passed"
	}
	if result.PhaseFPassed {
		status = "passed"
		lastErr = ""
	}
	finished := codexNowFn().UTC()
	_ = codexRecertSaveFn(home, codexroute.RecertState{
		SchemaVersion:      codexroute.RecertSchemaVersion,
		Status:             status,
		AttemptID:          attemptID,
		CodexVersion:       codexVersion,
		SlimferenceVersion: version,
		StartedAt:          started,
		FinishedAt:         finished,
		LastSuccessAt:      successTime(result.PhaseFPassed, finished),
		RetryAfter:         retryAfter(result.PhaseFPassed, finished),
		PhaseFPassed:       result.PhaseFPassed,
		BridgePassed:       result.BridgePassed,
		BytesC2S:           delta.WSS.BytesC2S,
		BytesS2C:           delta.WSS.BytesS2C,
		FramesForwarded:    delta.WSS.FramesForwarded,
		FramesReencoded:    delta.WSS.FramesReencoded,
		CompressedMutated:  delta.WSS.CompressedMessagesMutated,
		ParseFailures:      delta.WSS.ParseFailures,
		DegradedSessions:   delta.WSS.DegradedSessions,
		CompressionErrors:  delta.WSS.CompressionErrors,
		LastError:          lastErr,
	})
	codexRecertLogFn(home, fmt.Sprintf(
		"finish attempt=%s status=%s phasef=%v bridge=%v bytes_c2s=%d bytes_s2c=%d frames_forwarded=%d frames_reencoded=%d compressed_mutated=%d parse_failures=%d degraded_sessions=%d compression_errors=%d",
		attemptID,
		status,
		result.PhaseFPassed,
		result.BridgePassed,
		delta.WSS.BytesC2S,
		delta.WSS.BytesS2C,
		delta.WSS.FramesForwarded,
		delta.WSS.FramesReencoded,
		delta.WSS.CompressedMessagesMutated,
		delta.WSS.ParseFailures,
		delta.WSS.DegradedSessions,
		delta.WSS.CompressionErrors,
	))
	if flags.json {
		_ = emitCodexRecertifyResult(p.Out, result, true)
		if result.PhaseFPassed {
			return 0
		}
		return 1
	}
	if result.PhaseFPassed {
		fmt.Fprintf(p.Out, "Codex WSS recertified: %s (codex=%s slimference=%s)\n", result.CertificationPath, codexVersion, version)
		if result.BridgePassed {
			fmt.Fprintf(p.Out, "WSS bridge proof refreshed: %s\n", result.BridgeProofPath)
		}
		return 0
	}
	fmt.Fprintln(p.Err, "codex recertify: WSS Phase-F proof is not green")
	for _, f := range result.PhaseFFailures {
		fmt.Fprintf(p.Err, "  %s got=%s want=%s\n", f.Name, f.Got, f.Want)
	}
	if result.BridgePassed {
		fmt.Fprintf(p.Err, "codex recertify: WSS bridge proof passed and was written to %s\n", result.BridgeProofPath)
	}
	return 1
}

func parseCodexRecertifyFlags(args []string) (codexRecertifyFlags, error) {
	f := codexRecertifyFlags{host: "127.0.0.1", port: "8990", timeout: 180 * time.Second}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--help" || a == "-h":
			f.help = true
		case a == "--dry-run":
			f.dryRun = true
		case a == "--no-write":
			f.noWrite = true
		case a == "--force":
			f.force = true
		case a == "--json":
			f.json = true
		case strings.HasPrefix(a, "--host="):
			f.host = strings.TrimPrefix(a, "--host=")
		case strings.HasPrefix(a, "--port="):
			f.port = strings.TrimPrefix(a, "--port=")
		case strings.HasPrefix(a, "--timeout="):
			d, err := time.ParseDuration(strings.TrimPrefix(a, "--timeout="))
			if err != nil || d <= 0 {
				return f, fmt.Errorf("--timeout must be a positive duration")
			}
			f.timeout = d
		case strings.HasPrefix(a, "--operator="):
			f.operator = strings.TrimPrefix(a, "--operator=")
		case a == "--operator":
			if i+1 >= len(args) {
				return f, fmt.Errorf("--operator requires a value")
			}
			i++
			f.operator = args[i]
		case strings.HasPrefix(a, "--notes="):
			f.notes = strings.TrimPrefix(a, "--notes=")
		case a == "--notes":
			if i+1 >= len(args) {
				return f, fmt.Errorf("--notes requires a value")
			}
			i++
			f.notes = args[i]
		case strings.HasPrefix(a, "--"):
			return f, fmt.Errorf("unknown flag %q", a)
		default:
			if f.subject != "" {
				return f, fmt.Errorf("unexpected argument %q", a)
			}
			f.subject = a
		}
	}
	return f, nil
}

func defaultCodexRecertTrigger(input codexRecertTriggerInput) (codexRecertTriggerResult, error) {
	dir, err := os.MkdirTemp("", "slimference-codex-recert-*")
	if err != nil {
		return codexRecertTriggerResult{}, err
	}
	defer os.RemoveAll(dir)
	if err := seedCodexRecertRepo(dir); err != nil {
		return codexRecertTriggerResult{}, err
	}
	statusCmd := "git -C " + shellQuote(dir) + " status --short"
	prompts := []string{
		"Run exactly `" + statusCmd + "`, then reply exactly RECERT_DONE.",
	}
	for i, prompt := range prompts {
		args := []string{"codex", "run", "--transport=wss", "--host=" + input.Host, "--port=" + input.Port, "--"}
		if i == 0 {
			args = append(args, "exec", "--ignore-user-config", "--ephemeral", "--cd", dir, "--skip-git-repo-check", "--dangerously-bypass-approvals-and-sandbox", prompt)
		} else {
			args = append(args, "exec", "resume", "--last", "--ignore-user-config", "--ephemeral", "--skip-git-repo-check", "--dangerously-bypass-approvals-and-sandbox", prompt)
		}
		if err := recertRunCommandFn(input.Timeout, args...); err != nil {
			return codexRecertTriggerResult{PromptSequence: prompts}, err
		}
	}
	return codexRecertTriggerResult{PromptSequence: prompts}, nil
}

func seedCodexRecertRepo(dir string) error {
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("slimference recert\n"), 0o644); err != nil {
		return err
	}
	for i := range 160 {
		name := fmt.Sprintf("synthetic_%03d.go", i)
		body := "package synthetic\n// RECERT_LAYER0_STATUS_SENTINEL\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			return err
		}
	}
	if err := exec.Command("git", "-C", dir, "init", "-q").Run(); err != nil {
		return err
	}
	return nil
}

func runRecertCommand(timeout time.Duration, args ...string) error {
	bin, err := os.Executable()
	if err != nil {
		return err
	}
	stateDir, err := os.MkdirTemp("", "slimference-codex-recert-state-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stateDir)
	childConfigPath := filepath.Join(stateDir, "config.toml")
	childAnalyticsDir := filepath.Join(stateDir, "analytics")
	if err := os.MkdirAll(childAnalyticsDir, 0o700); err != nil {
		return err
	}
	childConfig := fmt.Sprintf("[analytics]\nlog_dir = %q\n\n[logging]\nfile = %q\n", childAnalyticsDir, "")
	if err := os.WriteFile(childConfigPath, []byte(childConfig), 0o600); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = append(os.Environ(), "SLIMFERENCE_CONFIG="+childConfigPath)
	var stdout strings.Builder
	var stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

func codexWSSBridgeFailures(state control.SetupState) []codexCertCriterion {
	criteria := []codexCertCriterion{
		{name: "wss.engine_active", got: fmt.Sprint(state.WSS.EngineActive), want: "true", pass: state.WSS.EngineActive},
		{name: "wss.mitm_bridged", got: fmt.Sprint(state.WSS.MITMBridged), want: ">0", pass: state.WSS.MITMBridged > 0},
		{name: "wss.bytes_c2s", got: fmt.Sprint(state.WSS.BytesC2S), want: ">0", pass: state.WSS.BytesC2S > 0},
		{name: "wss.bytes_s2c", got: fmt.Sprint(state.WSS.BytesS2C), want: ">0", pass: state.WSS.BytesS2C > 0},
		{name: "wss.frames", got: fmt.Sprint(state.WSS.C2SFrames + state.WSS.S2CFrames + state.WSS.FramesForwarded), want: ">0", pass: state.WSS.C2SFrames+state.WSS.S2CFrames+state.WSS.FramesForwarded > 0},
		{name: "wss.frames_reencoded", got: fmt.Sprint(state.WSS.FramesReencoded), want: "0", pass: state.WSS.FramesReencoded == 0},
		{name: "wss.compressed_messages_mutated", got: fmt.Sprint(state.WSS.CompressedMessagesMutated), want: "0", pass: state.WSS.CompressedMessagesMutated == 0},
		{name: "wss.parse_failures", got: fmt.Sprint(state.WSS.ParseFailures), want: "0", pass: state.WSS.ParseFailures == 0},
		{name: "wss.degraded_sessions", got: fmt.Sprint(state.WSS.DegradedSessions), want: "0", pass: state.WSS.DegradedSessions == 0},
		{name: "wss.compression_errors", got: fmt.Sprint(state.WSS.CompressionErrors), want: "0", pass: state.WSS.CompressionErrors == 0},
		{name: "codex_route.daemon_reachable", got: fmt.Sprint(state.CodexRoute.DaemonReachable), want: "true", pass: state.CodexRoute.DaemonReachable},
	}
	failures := make([]codexCertCriterion, 0, len(criteria))
	for _, c := range criteria {
		if !c.pass {
			failures = append(failures, c)
		}
	}
	return failures
}

func codexSetupDelta(before, after control.SetupState) control.SetupState {
	out := after
	out.WSS.PassthroughBridged = counterDelta(before.WSS.PassthroughBridged, after.WSS.PassthroughBridged)
	out.WSS.MITMBridged = counterDelta(before.WSS.MITMBridged, after.WSS.MITMBridged)
	out.WSS.PhasefBridged = counterDelta(before.WSS.PhasefBridged, after.WSS.PhasefBridged)
	out.WSS.Rejected = counterDelta(before.WSS.Rejected, after.WSS.Rejected)
	out.WSS.UpstreamDialFail = counterDelta(before.WSS.UpstreamDialFail, after.WSS.UpstreamDialFail)
	out.WSS.BytesC2S = counterDelta(before.WSS.BytesC2S, after.WSS.BytesC2S)
	out.WSS.BytesS2C = counterDelta(before.WSS.BytesS2C, after.WSS.BytesS2C)
	out.WSS.C2SFrames = counterDelta(before.WSS.C2SFrames, after.WSS.C2SFrames)
	out.WSS.S2CFrames = counterDelta(before.WSS.S2CFrames, after.WSS.S2CFrames)
	out.WSS.ParseFailures = counterDelta(before.WSS.ParseFailures, after.WSS.ParseFailures)
	out.WSS.DegradedSessions = counterDelta(before.WSS.DegradedSessions, after.WSS.DegradedSessions)
	out.WSS.FramesReencoded = counterDelta(before.WSS.FramesReencoded, after.WSS.FramesReencoded)
	out.WSS.FramesForwarded = counterDelta(before.WSS.FramesForwarded, after.WSS.FramesForwarded)
	out.WSS.CompressedMessagesInspected = counterDelta(before.WSS.CompressedMessagesInspected, after.WSS.CompressedMessagesInspected)
	out.WSS.CompressedMessagesMutated = counterDelta(before.WSS.CompressedMessagesMutated, after.WSS.CompressedMessagesMutated)
	out.WSS.CompressedMessagesBypassed = counterDelta(before.WSS.CompressedMessagesBypassed, after.WSS.CompressedMessagesBypassed)
	out.WSS.CompressionErrors = counterDelta(before.WSS.CompressionErrors, after.WSS.CompressionErrors)
	out.WSS.PhaseFRequests = counterDelta(before.WSS.PhaseFRequests, after.WSS.PhaseFRequests)
	out.WSS.PhaseFRequestBodies = counterDelta(before.WSS.PhaseFRequestBodies, after.WSS.PhaseFRequestBodies)
	out.WSS.PhaseFRequestMessagesIndexed = counterDelta(before.WSS.PhaseFRequestMessagesIndexed, after.WSS.PhaseFRequestMessagesIndexed)
	out.WSS.PhaseFTextDeltas = counterDelta(before.WSS.PhaseFTextDeltas, after.WSS.PhaseFTextDeltas)
	out.WSS.PhaseFTerminalResponses = counterDelta(before.WSS.PhaseFTerminalResponses, after.WSS.PhaseFTerminalResponses)
	out.WSS.PhaseFMutations = counterDelta(before.WSS.PhaseFMutations, after.WSS.PhaseFMutations)
	out.WSS.MutationActive = out.WSS.FramesReencoded > 0
	out.WSS.ByteBridgeOnly = out.WSS.FramesReencoded == 0 && out.WSS.FramesForwarded > 0
	return out
}

func counterDelta(before, after int64) int64 {
	if after >= before {
		return after - before
	}
	return after
}

func codexCertFromState(state control.SetupState, codexVersion string, flags codexRecertifyFlags) codexroute.CertificationState {
	return codexroute.CertificationState{
		SchemaVersion:      codexroute.CertificationSchemaVersion,
		Transport:          string(codexroute.TransportWSS),
		RouteProfile:       codexroute.RouteProfileScopedRawWSS,
		CodexVersion:       codexVersion,
		SlimferenceVersion: version,
		Passed:             true,
		FramesReencoded:    state.WSS.FramesReencoded,
		DegradedSessions:   0,
		ParseFailures:      0,
		Timestamp:          codexNowFn().UTC(),
		Operator:           flags.operator,
		Notes:              flags.notes,
	}
}

func codexBridgeProofFromState(state control.SetupState, codexVersion string, flags codexRecertifyFlags) codexroute.BridgeProofState {
	return codexroute.BridgeProofState{
		SchemaVersion:      codexroute.CertificationSchemaVersion,
		Transport:          string(codexroute.TransportWSS),
		RouteProfile:       codexroute.RouteProfileScopedWSSBridge,
		CodexVersion:       codexVersion,
		SlimferenceVersion: version,
		Passed:             true,
		BytesC2S:           state.WSS.BytesC2S,
		BytesS2C:           state.WSS.BytesS2C,
		C2SFrames:          state.WSS.C2SFrames,
		S2CFrames:          state.WSS.S2CFrames,
		FramesForwarded:    state.WSS.FramesForwarded,
		FramesReencoded:    state.WSS.FramesReencoded,
		DegradedSessions:   state.WSS.DegradedSessions,
		ParseFailures:      state.WSS.ParseFailures,
		CompressionErrors:  state.WSS.CompressionErrors,
		Timestamp:          codexNowFn().UTC(),
		Operator:           flags.operator,
		Notes:              flags.notes,
	}
}

func emitCodexRecertifyResult(w io.Writer, result codexRecertifyResult, asJSON bool) int {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(result)
		return 0
	}
	fmt.Fprintf(w, "Codex WSS recert plan: codex=%s slimference=%s cert=%s bridge=%s\n",
		result.CodexVersion, result.SlimferenceVersion, result.CertificationPath, result.BridgeProofPath)
	return 0
}

func finishCodexRecertError(p installPrinter, flags codexRecertifyFlags, home, attemptID, codexVersion, msg string) int {
	now := codexNowFn().UTC()
	_ = codexRecertSaveFn(home, codexroute.RecertState{
		SchemaVersion:      codexroute.RecertSchemaVersion,
		Status:             "failed",
		AttemptID:          attemptID,
		CodexVersion:       codexVersion,
		SlimferenceVersion: version,
		FinishedAt:         now,
		RetryAfter:         now.Add(30 * time.Minute),
		LastError:          msg,
	})
	codexRecertLogFn(home, "error attempt="+attemptID+" "+msg)
	if flags.json {
		enc := json.NewEncoder(p.Out)
		enc.SetIndent("", "  ")
		_ = enc.Encode(map[string]string{"attempt_id": attemptID, "error": msg})
	} else {
		fmt.Fprintf(p.Err, "codex recertify: %s\n", msg)
	}
	return 1
}

func acquireCodexRecertLock(home string) (func(), error) {
	path := codexroute.RecertLockPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			if staleCodexRecertLock(path) {
				_ = os.Remove(path)
				return acquireCodexRecertLock(home)
			}
			return nil, errCodexRecertLocked
		}
		return nil, err
	}
	_, _ = fmt.Fprintf(f, "pid=%d started=%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339Nano))
	_ = f.Close()
	return func() { _ = os.Remove(path) }, nil
}

func staleCodexRecertLock(path string) bool {
	st, err := os.Stat(path)
	if err != nil {
		return false
	}
	if codexNowFn().Sub(st.ModTime()) > 30*time.Minute {
		return true
	}
	pid, ok := readCodexRecertLockPID(path)
	if !ok || pid <= 0 || pid == os.Getpid() {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return true
	}
	err = proc.Signal(syscall.Signal(0))
	return errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH)
}

func readCodexRecertLockPID(path string) (int, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	for field := range strings.FieldsSeq(string(data)) {
		if !strings.HasPrefix(field, "pid=") {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimPrefix(field, "pid="))
		if err != nil {
			return 0, false
		}
		return pid, true
	}
	return 0, false
}

func codexCriterionOutputs(in []codexCertCriterion) []codexCriterionOutput {
	if len(in) == 0 {
		return nil
	}
	out := make([]codexCriterionOutput, 0, len(in))
	for _, c := range in {
		out = append(out, codexCriterionOutput{Name: c.name, Got: c.got, Want: c.want})
	}
	return out
}

func codexRecertBackoffActive(home string, force bool) (bool, string) {
	if force {
		return false, ""
	}
	state, exists, err := codexroute.LoadRecertState(home)
	if err != nil || !exists || state.RetryAfter.IsZero() {
		return false, ""
	}
	if codexNowFn().Before(state.RetryAfter) {
		return true, "recert backoff active until " + state.RetryAfter.Format(time.RFC3339)
	}
	return false, ""
}

func startCodexAutoRecert(home, host, port string, decision codexroute.AutoDecision) {
	if os.Getenv("SLIMFERENCE_CODEX_AUTO_RECERT") == "0" || !decision.NeedsRecert {
		return
	}
	if decision.RecertStatus == "running" {
		return
	}
	if !decision.RecertRetryAfter.IsZero() && codexNowFn().Before(decision.RecertRetryAfter) {
		return
	}
	bin, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(bin, "codex", "recertify", "wss",
		"--operator=auto",
		"--notes=auto recert after WSS proof drift",
		"--host="+host,
		"--port="+port,
	)
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err == nil {
		cmd.Stdout = devNull
		cmd.Stderr = devNull
	}
	if err := cmd.Start(); err == nil {
		codexRecertLogFn(home, "auto-start pid="+fmt.Sprint(cmd.Process.Pid)+" reason="+decision.FallbackReason)
	}
	if devNull != nil {
		_ = devNull.Close()
	}
}

var (
	daemonCodexAutoRecertFn   = maybeStartDaemonCodexAutoRecert
	daemonAutoRecertAllowedFn = func() bool {
		return !strings.HasSuffix(os.Args[0], ".test")
	}
)

func maybeStartDaemonCodexAutoRecert(port int) {
	if os.Getenv("SLIMFERENCE_DAEMON_AUTO_RECERT") == "0" || !daemonAutoRecertAllowedFn() {
		return
	}
	home, err := codexRouteHomeFn()
	if err != nil || home == "" {
		return
	}
	decision := codexAutoFn(home)
	if !decision.NeedsRecert {
		return
	}
	codexAutoRecertFn(home, "127.0.0.1", fmt.Sprint(port), decision)
}

func appendCodexRecertLog(home, line string) {
	_ = appendBoundedCodexLog(codexroute.RecertLogPath(home), []byte(codexNowFn().UTC().Format(time.RFC3339Nano)+" "+line+"\n"))
}

func appendBoundedCodexLog(path string, line []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if st, err := os.Stat(path); err == nil && st.Size()+int64(len(line)) > codexRecertLogMaxBytes {
		_ = os.Remove(path + ".1")
		if err := os.Rename(path, path+".1"); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(line)
	return err
}

func successTime(ok bool, t time.Time) time.Time {
	if ok {
		return t
	}
	return time.Time{}
}

func retryAfter(ok bool, t time.Time) time.Time {
	if ok {
		return time.Time{}
	}
	return t.Add(30 * time.Minute)
}
