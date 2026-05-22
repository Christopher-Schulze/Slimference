package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/slimference/slimference/internal/codexroute"
	"github.com/slimference/slimference/internal/control"
	"github.com/slimference/slimference/internal/proxy"
	"github.com/slimference/slimference/internal/tlsca"
	"github.com/slimference/slimference/internal/transparent"
)

var (
	codexRouteHomeFn      = os.UserHomeDir
	codexRouteEnableFn    = codexroute.EnableWithOptions
	codexRouteDisableFn   = codexroute.Disable
	codexRouteInspectFn   = codexroute.InspectWithOptions
	codexRouteHealthFn    = defaultProxyHealthCheck
	codexProxyRunFn       = proxyRun
	codexVersionFn        = currentCodexVersion
	codexAutoFn           = resolveCodexAutoTransport
	codexCertSaveFn       = codexroute.SaveCertification
	codexBridgeSaveFn     = codexroute.SaveBridgeProof
	codexRecertSaveFn     = codexroute.SaveRecertState
	codexAutoRecertFn     = startCodexAutoRecert
	codexRecertTriggerFn  = defaultCodexRecertTrigger
	codexRecertLogFn      = appendCodexRecertLog
	codexSetupStateFn     = fetchCodexSetupState
	codexVersionOutFn     = defaultCodexCLIVersionOutput
	codexNowFn            = time.Now
	codexDesktopCleanupFn = cleanupCodexDesktopProcess
	codexDesktopSessionFn = codexDesktopProofSessionPath
	codexDesktopResultFn  = codexDesktopProofResultPath
)

type codexRouteFlags struct {
	host      string
	port      string
	transport string
	json      bool
	dryRun    bool
	help      bool
}

type codexCertifyFlags struct {
	subject  string
	host     string
	port     string
	operator string
	notes    string
	dryRun   bool
	help     bool
}

type codexCertCriterion struct {
	name string
	got  string
	want string
	pass bool
}

type codexDesktopStatusFlags struct {
	host string
	port string
	json bool
	help bool
}

type codexDesktopProveFlags struct {
	host     string
	port     string
	duration time.Duration
	json     bool
	keepOpen bool
	manual   bool
	finish   bool
	help     bool
}

type codexDesktopStatusOutput struct {
	Mode                 string                   `json:"mode"`
	FailureClass         string                   `json:"failure_class,omitempty"`
	ProxyURL             string                   `json:"proxy_url"`
	CATrust              codexDesktopCAState      `json:"ca_trust"`
	DaemonReachable      bool                     `json:"daemon_reachable"`
	DaemonError          string                   `json:"daemon_error,omitempty"`
	WSS                  control.WSSState         `json:"wss"`
	WSSCountersScope     string                   `json:"wss_counters_scope"`
	LiveProofRequired    bool                     `json:"live_proof_required"`
	ConversationObserved bool                     `json:"conversation_observed"`
	LaunchCommand        string                   `json:"launch_command"`
	LastProof            *codexDesktopProofOutput `json:"last_proof,omitempty"`
	Notes                []string                 `json:"notes,omitempty"`
}

type codexDesktopProofOutput struct {
	Mode              string              `json:"mode"`
	FailureClass      string              `json:"failure_class,omitempty"`
	Transport         string              `json:"transport,omitempty"`
	Duration          string              `json:"duration"`
	LaunchPID         int                 `json:"launch_pid,omitempty"`
	LaunchOutput      string              `json:"launch_output,omitempty"`
	DeltaWSS          control.WSSState    `json:"delta_wss"`
	CATrust           codexDesktopCAState `json:"ca_trust"`
	SessionPath       string              `json:"session_path,omitempty"`
	CleanupAttempted  bool                `json:"cleanup_attempted"`
	CleanupError      string              `json:"cleanup_error,omitempty"`
	LaunchReady       bool                `json:"launch_ready"`
	DesktopProven     bool                `json:"desktop_proven"`
	DesktopSavings    bool                `json:"desktop_savings"`
	ManualPromptStill bool                `json:"manual_prompt_still_required"`
	Notes             []string            `json:"notes,omitempty"`
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
}

func handleCodexCmd(args []string) {
	exitFn(runCodexCmd(args, defaultInstallPrinter()))
}

func runCodexCmd(args []string, p installPrinter) int {
	if len(args) == 0 {
		fmt.Fprint(p.Out, codexHelpText)
		return 0
	}
	switch args[0] {
	case "run":
		return runCodexRunCmd(args[1:], p)
	case "enable":
		return runCodexEnableCmd(args[1:], p)
	case "disable":
		return runCodexDisableCmd(args[1:], p)
	case "status":
		return runCodexStatusCmd(args[1:], p)
	case "certify":
		return runCodexCertifyCmd(args[1:], p)
	case "recertify":
		return runCodexRecertifyCmd(args[1:], p)
	case "desktop":
		return runCodexDesktopCmd(args[1:], p)
	case "launch-desktop":
		return runCodexLaunchDesktopCmd(args[1:], p)
	case "--help", "-h", "help":
		fmt.Fprint(p.Out, codexHelpText)
		return 0
	default:
		fmt.Fprintf(p.Err, "codex: unknown subcommand %q\n", args[0])
		fmt.Fprint(p.Err, codexUsageLine)
		return 2
	}
}

func runCodexRunCmd(args []string, p installPrinter) int {
	flags, direct, codexArgs := parseCodexRunFlags(args)
	var autoHome string
	var autoDecision codexroute.AutoDecision
	autoNeedsRecert := false
	if flags.help {
		fmt.Fprint(p.Out, codexRunHelpText)
		return 0
	}
	if strings.HasPrefix(flags.transport, "invalid:") {
		fmt.Fprintf(p.Err, "codex run: transport must be auto, http, wss, wss-bridge, or direct\n")
		return 2
	}
	if flags.transport == "auto" {
		home, err := codexRouteHomeFn()
		if err == nil && home != "" {
			decision := codexAutoFn(home)
			autoHome = home
			autoDecision = decision
			autoNeedsRecert = decision.NeedsRecert
			flags.transport = string(decision.Transport)
			if decision.Mode == codexroute.AutoModeWSSBridge {
				flags.transport = string(codexroute.TransportWSS) + "-bridge"
			}
			if decision.FallbackReason != "" {
				fmt.Fprintf(p.Err, "codex run: auto transport -> %s (%s)\n",
					decision.Mode, decision.FallbackReason)
			}
		} else {
			flags.transport = string(codexroute.TransportHTTP)
			fmt.Fprintln(p.Err, "codex run: auto transport -> http (HOME unresolved)")
		}
	}
	mode := "proxied"
	if direct || flags.transport == "direct" {
		mode = "direct"
	} else if err := codexRouteHealthFn(flags.host, flags.port); err != nil {
		fmt.Fprintf(p.Err, "codex run: Slimference daemon unreachable at %s:%s (%v); falling back to direct Codex.\n",
			flags.host, flags.port, err)
		mode = "direct"
	} else if flags.transport == "wss" {
		mode = "proxied-wss"
	} else if flags.transport == "wss-bridge" {
		mode = "proxied-wss-bridge"
	}
	if mode != "direct" && autoNeedsRecert && autoHome != "" {
		codexAutoRecertFn(autoHome, flags.host, flags.port, autoDecision)
	}
	proxyArgs := []string{"run", "codex", "--" + mode, "--host=" + flags.host, "--port=" + flags.port}
	if len(codexArgs) > 0 {
		proxyArgs = append(proxyArgs, "--")
		proxyArgs = append(proxyArgs, codexArgs...)
	}
	return codexProxyRunFn(proxyArgs, codexProxyEnv(p))
}

func runCodexEnableCmd(args []string, p installPrinter) int {
	flags, err := parseCodexRouteFlags(args)
	if err != nil {
		fmt.Fprintf(p.Err, "codex enable: %v\n", err)
		return 2
	}
	if flags.help {
		fmt.Fprint(p.Out, codexEnableHelpText)
		return 0
	}
	home, err := codexRouteHomeFn()
	if err != nil || home == "" {
		fmt.Fprintln(p.Err, "codex enable: HOME unresolved")
		return 1
	}
	if flags.transport == "auto" {
		decision := codexAutoFn(home)
		flags.transport = string(decision.Transport)
		if decision.FallbackReason != "" {
			fmt.Fprintf(p.Out, "Auto transport -> %s (%s)\n", flags.transport, decision.FallbackReason)
		} else {
			fmt.Fprintf(p.Out, "Auto transport -> %s (certified)\n", flags.transport)
		}
	}
	proxyURL := codexroute.ProxyURL(flags.host, flags.port)
	if flags.dryRun {
		fmt.Fprintf(p.Out, "Dry-run: would write scoped Codex route to %s\n\n%s",
			codexroute.ConfigPath(home), codexroute.PreviewBlockWithOptions(proxyURL, codexRouteOptions(flags.transport)))
		return 0
	}
	evt, err := codexRouteEnableFn(home, proxyURL, codexRouteOptions(flags.transport))
	if err != nil {
		fmt.Fprintf(p.Err, "codex enable: %v\n", err)
		return 1
	}
	if evt.Action == "skipped_codex_config_absent" {
		fmt.Fprintf(p.Err, "codex enable: %s does not exist; run Codex once or `slimference install` first. No files changed.\n", evt.Path)
		return 1
	}
	fmt.Fprintf(p.Out, "Codex route enabled: %s (%s)\n", evt.Path, evt.Action)
	fmt.Fprintln(p.Out, "Scope: Codex CLI + Codex Desktop App only. Browser ChatGPT and ChatGPT.app stay direct.")
	fmt.Fprintln(p.Out, "Desktop App: restart Codex.app/app-server so it reloads ~/.codex/config.toml.")
	return 0
}

func runCodexDisableCmd(args []string, p installPrinter) int {
	flags, err := parseCodexRouteFlags(args)
	if err != nil {
		fmt.Fprintf(p.Err, "codex disable: %v\n", err)
		return 2
	}
	if flags.help {
		fmt.Fprint(p.Out, codexDisableHelpText)
		return 0
	}
	home, err := codexRouteHomeFn()
	if err != nil || home == "" {
		fmt.Fprintln(p.Err, "codex disable: HOME unresolved")
		return 1
	}
	if flags.dryRun {
		fmt.Fprintf(p.Out, "Dry-run: would remove scoped Codex route from %s\n", codexroute.ConfigPath(home))
		return 0
	}
	evt, err := codexRouteDisableFn(home)
	if err != nil {
		fmt.Fprintf(p.Err, "codex disable: %v\n", err)
		return 1
	}
	fmt.Fprintf(p.Out, "Codex route disabled: %s (%s)\n", evt.Path, evt.Action)
	fmt.Fprintln(p.Out, "Codex now talks direct after the next CLI/App config reload.")
	return 0
}

func runCodexStatusCmd(args []string, p installPrinter) int {
	flags, err := parseCodexRouteFlags(args)
	if err != nil {
		fmt.Fprintf(p.Err, "codex status: %v\n", err)
		return 2
	}
	if flags.help {
		fmt.Fprint(p.Out, codexStatusHelpText)
		return 0
	}
	home, err := codexRouteHomeFn()
	if err != nil || home == "" {
		fmt.Fprintln(p.Err, "codex status: HOME unresolved")
		return 1
	}
	proxyURL := codexroute.ProxyURL(flags.host, flags.port)
	status, err := codexRouteInspectFn(home, proxyURL, codexroute.Options{})
	if err != nil {
		fmt.Fprintf(p.Err, "codex status: %v\n", err)
		return 1
	}
	healthErr := codexRouteHealthFn(flags.host, flags.port)
	out := struct {
		Route  codexroute.Status       `json:"route"`
		Auto   codexroute.AutoDecision `json:"auto"`
		Daemon struct {
			Reachable bool   `json:"reachable"`
			Error     string `json:"error,omitempty"`
		} `json:"daemon"`
	}{Route: status, Auto: codexAutoFn(home)}
	out.Daemon.Reachable = healthErr == nil
	if healthErr != nil {
		out.Daemon.Error = healthErr.Error()
	}
	if flags.json {
		enc := json.NewEncoder(p.Out)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
		return 0
	}
	renderCodexStatus(p.Out, out.Route, out.Daemon.Reachable, out.Daemon.Error, out.Auto)
	return 0
}

func runCodexCertifyCmd(args []string, p installPrinter) int {
	flags, err := parseCodexCertifyFlags(args)
	if err != nil {
		fmt.Fprintf(p.Err, "codex certify: %v\n", err)
		return 2
	}
	if flags.help {
		fmt.Fprint(p.Out, codexCertifyHelpText)
		return 0
	}
	if flags.subject != "wss" {
		fmt.Fprintln(p.Err, "codex certify: subject must be wss")
		return 2
	}
	home, err := codexRouteHomeFn()
	if err != nil || home == "" {
		fmt.Fprintln(p.Err, "codex certify: HOME unresolved")
		return 1
	}
	versionOut, err := codexVersionOutFn()
	if err != nil {
		fmt.Fprintf(p.Err, "codex certify: codex --version failed: %v\n", err)
		return 1
	}
	codexVersion, err := parseCodexCLIVersion(versionOut)
	if err != nil {
		fmt.Fprintf(p.Err, "codex certify: %v\n", err)
		return 1
	}
	state, err := codexSetupStateFn(flags.host, flags.port, 2*time.Second)
	if err != nil {
		fmt.Fprintf(p.Err, "codex certify: admin state unavailable at %s:%s: %v\n", flags.host, flags.port, err)
		return 1
	}
	failures := codexWSSCertificationFailures(state)
	if len(failures) > 0 {
		fmt.Fprintln(p.Err, "codex certify: WSS proof is not green")
		for _, f := range failures {
			fmt.Fprintf(p.Err, "  %s got=%s want=%s\n", f.name, f.got, f.want)
		}
		return 1
	}
	cert := codexroute.CertificationState{
		SchemaVersion:      codexroute.CertificationSchemaVersion,
		Transport:          string(codexroute.TransportWSS),
		RouteProfile:       codexroute.RouteProfileScopedRawWSS,
		CodexVersion:       codexVersion,
		SlimferenceVersion: version,
		Passed:             true,
		FramesReencoded:    state.WSS.FramesReencoded,
		DegradedSessions:   0,
		ParseFailures:      0,
		LastError:          "",
		Timestamp:          codexNowFn().UTC(),
		Operator:           flags.operator,
		Notes:              flags.notes,
	}
	if flags.dryRun {
		enc := json.NewEncoder(p.Out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(cert); err != nil {
			fmt.Fprintf(p.Err, "codex certify: encode dry-run JSON: %v\n", err)
			return 1
		}
		return 0
	}
	if err := codexCertSaveFn(home, cert); err != nil {
		fmt.Fprintf(p.Err, "codex certify: write certification: %v\n", err)
		return 1
	}
	if len(codexWSSBridgeFailures(state)) == 0 {
		bridgeFlags := codexRecertifyFlags{operator: flags.operator, notes: flags.notes}
		if err := codexBridgeSaveFn(home, codexBridgeProofFromState(state, codexVersion, bridgeFlags)); err != nil {
			fmt.Fprintf(p.Err, "codex certify: write bridge proof: %v\n", err)
			return 1
		}
	}
	fmt.Fprintf(p.Out, "Codex WSS certification written: %s (codex=%s slimference=%s)\n",
		codexroute.CertificationPath(home), codexVersion, version)
	fmt.Fprintf(p.Out, "Live frames_reencoded at issue: %d\n", state.WSS.FramesReencoded)
	fmt.Fprintln(p.Out, "Run `slimference codex status` to confirm wss_certified=true.")
	return 0
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

	before, err := codexSetupStateFn(flags.host, flags.port, 2*time.Second)
	if err != nil {
		out := codexDesktopProofOutput{
			Mode:         "daemon_unreachable",
			FailureClass: "daemon_unreachable",
			Duration:     flags.duration.String(),
			Notes:        []string{"start the Slimference daemon before running the Desktop proof"},
		}
		emitCodexDesktopProof(p, flags.json, out)
		return 1
	}

	var launchOut, launchErr strings.Builder
	rc := runCodexLaunchDesktopCmd([]string{"--transport=app-server", "--replace-existing", "--host=" + flags.host, "--port=" + flags.port}, installPrinter{Out: &launchOut, Err: &launchErr})
	out := codexDesktopProofOutput{
		Duration:          flags.duration.String(),
		Transport:         codexDesktopTransportAppServer,
		LaunchOutput:      strings.TrimSpace(launchOut.String()),
		ManualPromptStill: true,
		Notes: []string{
			"automated proof covers launch-time app-server shim routing",
			"full Desktop savings proof still needs a prompt-tied WSS delta if launch-time bytes do not flow",
		},
	}
	out.LaunchPID = parseCodexDesktopLaunchPID(out.LaunchOutput)
	if rc != 0 {
		out.Mode = "launch_failed"
		out.FailureClass = "launch_failed"
		msg := strings.TrimSpace(launchErr.String())
		if msg == "" {
			msg = out.LaunchOutput
		}
		if msg != "" {
			out.Notes = append(out.Notes, msg)
		}
		emitCodexDesktopProof(p, flags.json, out)
		return 1
	}

	time.Sleep(flags.duration)
	after, err := codexSetupStateFn(flags.host, flags.port, 2*time.Second)
	if err != nil {
		out.Mode = "post_probe_failed"
		out.FailureClass = "post_probe_failed"
		out.Notes = append(out.Notes, err.Error())
		cleanupCodexDesktopProof(&out, flags.keepOpen)
		emitCodexDesktopProof(p, flags.json, out)
		return 1
	}
	out.DeltaWSS = codexSetupDelta(before, after).WSS
	classifyCodexDesktopProof(&out, flags.manual)
	if flags.manual && (out.LaunchReady || out.DesktopSavings) {
		if err := writeCodexDesktopProofSession(flags, before.WSS, &out); err != nil {
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
	after, err := codexSetupStateFn(session.Host, session.Port, 2*time.Second)
	if err != nil {
		out := codexDesktopProofOutput{
			Mode:         "daemon_unreachable",
			FailureClass: "daemon_unreachable",
			Duration:     time.Since(session.StartedAt).Round(time.Second).String(),
			LaunchPID:    session.LaunchPID,
			SessionPath:  sessionPath,
			Notes:        []string{err.Error()},
		}
		emitCodexDesktopProof(p, flags.json, out)
		return 1
	}
	before := control.SetupState{WSS: session.BaselineWSS}
	out := codexDesktopProofOutput{
		Duration:     time.Since(session.StartedAt).Round(time.Second).String(),
		Transport:    firstNonEmpty(session.Transport, codexDesktopTransportAppServer),
		LaunchPID:    session.LaunchPID,
		LaunchOutput: session.LaunchOutput,
		SessionPath:  sessionPath,
		DeltaWSS:     codexSetupDelta(before, after).WSS,
		Notes:        []string{"finish compares current daemon WSS state to the manual Desktop proof baseline"},
	}
	classifyCodexDesktopProof(&out, false)
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
		case a == "--manual":
			f.manual = true
			f.keepOpen = true
		case a == "--finish":
			f.finish = true
		case strings.HasPrefix(a, "--host="):
			f.host = strings.TrimPrefix(a, "--host=")
		case strings.HasPrefix(a, "--port="):
			f.port = strings.TrimPrefix(a, "--port=")
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
	return f, nil
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

func writeCodexDesktopProofSession(flags codexDesktopProveFlags, baseline control.WSSState, out *codexDesktopProofOutput) error {
	path := codexDesktopSessionFn()
	session := codexDesktopProofSession{
		SchemaVersion: 1,
		Host:          flags.host,
		Port:          flags.port,
		Transport:     out.Transport,
		LaunchPID:     out.LaunchPID,
		StartedAt:     codexNowFn().UTC(),
		BaselineWSS:   baseline,
		LaunchOutput:  out.LaunchOutput,
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
	if out.LaunchPID > 0 {
		fmt.Fprintf(w, "  PID       %d\n", out.LaunchPID)
	}
	if out.Transport != "" {
		fmt.Fprintf(w, "  Transport %s\n", out.Transport)
	}
	if out.SessionPath != "" {
		fmt.Fprintf(w, "  Session   %s\n", out.SessionPath)
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

func parseCodexDesktopLaunchPID(text string) int {
	var pid int
	if _, err := fmt.Sscanf(text, "Codex.app launched (PID %d)", &pid); err == nil {
		return pid
	}
	return 0
}

func parseCodexRouteFlags(args []string) (codexRouteFlags, error) {
	f := codexRouteFlags{host: "127.0.0.1", port: "8990", transport: "auto"}
	for _, a := range args {
		switch {
		case a == "--help" || a == "-h":
			f.help = true
		case a == "--json":
			f.json = true
		case a == "--dry-run":
			f.dryRun = true
		case strings.HasPrefix(a, "--host="):
			f.host = strings.TrimPrefix(a, "--host=")
		case strings.HasPrefix(a, "--port="):
			f.port = strings.TrimPrefix(a, "--port=")
		case strings.HasPrefix(a, "--transport="):
			t := strings.TrimPrefix(a, "--transport=")
			if t != "auto" && t != "http" && t != "wss" {
				return f, fmt.Errorf("transport must be auto, http or wss")
			}
			f.transport = t
		default:
			return f, fmt.Errorf("unknown flag %q", a)
		}
	}
	return f, nil
}

func parseCodexRunFlags(args []string) (codexRouteFlags, bool, []string) {
	f := codexRouteFlags{host: "127.0.0.1", port: "8990", transport: "auto"}
	direct := false
	var codexArgs []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			codexArgs = append(codexArgs, args[i+1:]...)
			return f, direct, codexArgs
		case a == "--help" || a == "-h":
			f.help = true
		case a == "--direct":
			direct = true
			f.transport = "direct"
		case strings.HasPrefix(a, "--host="):
			f.host = strings.TrimPrefix(a, "--host=")
		case strings.HasPrefix(a, "--port="):
			f.port = strings.TrimPrefix(a, "--port=")
		case strings.HasPrefix(a, "--transport="):
			t := strings.TrimPrefix(a, "--transport=")
			if t == "auto" || t == "http" || t == "wss" || t == "wss-bridge" || t == "direct" {
				f.transport = t
				continue
			}
			f.transport = "invalid:" + t
		default:
			codexArgs = append(codexArgs, a)
		}
	}
	return f, direct, codexArgs
}

func parseCodexCertifyFlags(args []string) (codexCertifyFlags, error) {
	f := codexCertifyFlags{host: "127.0.0.1", port: "8990"}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--help" || a == "-h":
			f.help = true
		case a == "--dry-run":
			f.dryRun = true
		case strings.HasPrefix(a, "--host="):
			f.host = strings.TrimPrefix(a, "--host=")
		case strings.HasPrefix(a, "--port="):
			f.port = strings.TrimPrefix(a, "--port=")
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
	out := codexDesktopStatusOutput{
		Mode:              "not_ready",
		ProxyURL:          proxyURL,
		CATrust:           codexDesktopCATrustFn(),
		WSSCountersScope:  "daemon_cumulative_not_desktop_proof",
		LiveProofRequired: true,
		LaunchCommand:     "slimference codex launch-desktop --transport=app-server --replace-existing",
	}
	if last, err := readCodexDesktopProofResult(codexDesktopResultFn()); err == nil {
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
	if out.LastProof != nil {
		applyCodexDesktopLastProof(&out, out.LastProof)
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

func applyCodexDesktopLastProof(out *codexDesktopStatusOutput, last *codexDesktopProofOutput) {
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
		out.Notes = append(out.Notes, "last Desktop app-server shim proof was green with Phase-F mutation")
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
		out.Notes = append(out.Notes, "last Desktop proof launched successfully but still needs a prompt plus `slimference codex desktop prove --finish --json`")
	case "desktop_ca_env_rejected":
		out.Mode = "desktop_direct_only"
		out.FailureClass = firstNonEmpty(last.FailureClass, "tls_trust_rejected")
		out.Notes = append(out.Notes, "last Desktop proof reached Slimference CONNECT but closed before application bytes; use normal Codex.app direct launch")
	case "desktop_no_wss_delta":
		out.Mode = "desktop_direct_only"
		out.FailureClass = firstNonEmpty(last.FailureClass, "no_wss_delta")
		out.Notes = append(out.Notes, "last Desktop proof produced no Desktop WSS delta; use normal Codex.app direct launch")
	case "desktop_app_server_wss_bridge":
		out.Mode = "desktop_wss_bridge_only"
		out.FailureClass = "desktop_savings_not_proven"
		out.ConversationObserved = true
		out.Notes = append(out.Notes, "last Desktop proof carried WSS bytes but did not prove Phase-F savings")
	}
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
	fmt.Fprintf(w, "  Proof     live_required=%v conversation_observed=%v\n", out.LiveProofRequired, out.ConversationObserved)
	fmt.Fprintf(w, "  Launch    %s\n", out.LaunchCommand)
	for _, note := range out.Notes {
		fmt.Fprintf(w, "  Note      %s\n", note)
	}
}

func codexRouteOptions(transport string) codexroute.Options {
	if transport == "wss" {
		return codexroute.Options{Transport: codexroute.TransportWSS}
	}
	return codexroute.Options{Transport: codexroute.TransportHTTP}
}

func resolveCodexAutoTransport(home string) codexroute.AutoDecision {
	decision, _ := codexroute.DecideAutoTransport(home, codexVersionFn(), version)
	return decision
}

func currentCodexVersion() string {
	out, err := codexVersionOutFn()
	if err != nil {
		return "unknown"
	}
	version, err := parseCodexCLIVersion(out)
	if err != nil {
		return "unknown"
	}
	return version
}

func defaultCodexCLIVersionOutput() ([]byte, error) {
	bin := strings.TrimSpace(os.Getenv("CODEX_BIN"))
	if bin == "" {
		bin = "codex"
	}
	return exec.Command(bin, "--version").Output()
}

func parseCodexCLIVersion(out []byte) (string, error) {
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) < 2 {
			return "", fmt.Errorf("unexpected codex --version output %q", strings.TrimSpace(line))
		}
		return fields[1], nil
	}
	return "", fmt.Errorf("empty codex --version output")
}

func fetchCodexSetupState(host, port string, timeout time.Duration) (control.SetupState, error) {
	addr := net.JoinHostPort(host, port)
	url := "http://" + addr + proxy.AdminStatePath
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return control.SetupState{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return control.SetupState{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return control.SetupState{}, fmt.Errorf("admin returned %d", resp.StatusCode)
	}
	var state control.SetupState
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
		return control.SetupState{}, err
	}
	return state, nil
}

func codexWSSCertificationFailures(state control.SetupState) []codexCertCriterion {
	criteria := []codexCertCriterion{
		{name: "wss.parse_failures", got: fmt.Sprint(state.WSS.ParseFailures), want: "0", pass: state.WSS.ParseFailures == 0},
		{name: "wss.degraded_sessions", got: fmt.Sprint(state.WSS.DegradedSessions), want: "0", pass: state.WSS.DegradedSessions == 0},
		{name: "wss.compression_errors", got: fmt.Sprint(state.WSS.CompressionErrors), want: "0", pass: state.WSS.CompressionErrors == 0},
		{name: "wss.frames_reencoded", got: fmt.Sprint(state.WSS.FramesReencoded), want: ">0", pass: state.WSS.FramesReencoded > 0},
		{name: "wss.compressed_messages_mutated", got: fmt.Sprint(state.WSS.CompressedMessagesMutated), want: ">0", pass: state.WSS.CompressedMessagesMutated > 0},
		{name: "wss.mutation_active", got: fmt.Sprint(state.WSS.MutationActive), want: "true", pass: state.WSS.MutationActive},
		{name: "wss.byte_bridge_only", got: fmt.Sprint(state.WSS.ByteBridgeOnly), want: "false", pass: !state.WSS.ByteBridgeOnly},
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

func codexProxyEnv(p installPrinter) proxyEnv {
	home := os.Getenv("HOME")
	caDir := filepath.Join(home, ".slimference")
	return proxyEnv{
		Stdout:      p.Out,
		Stderr:      p.Err,
		Stdin:       os.Stdin,
		Home:        home,
		CADirFn:     func() string { return caDir },
		Network:     transparent.NewManager(),
		Keychain:    transparent.NewKeychain(),
		Launch:      transparent.NewLaunchAgent(),
		LoadCA:      tlsca.LoadOrGenerateCA,
		HealthCheck: defaultProxyHealthCheck,
		RunCommand:  defaultProxyCommandRunner,
	}
}

func renderCodexStatus(w io.Writer, s codexroute.Status, daemonReachable bool, daemonErr string, auto codexroute.AutoDecision) {
	fmt.Fprintln(w, "Slimference Codex route")
	fmt.Fprintln(w, "-----------------------")
	fmt.Fprintf(w, "  Config   exists=%v enabled=%v complete=%v\n", s.Exists, s.Enabled, s.Complete)
	fmt.Fprintf(w, "  Path     %s\n", s.Path)
	fmt.Fprintf(w, "  BaseURL  %s\n", s.BaseURL)
	if s.Transport != "" {
		fmt.Fprintf(w, "  Transport %s\n", s.Transport)
	}
	fmt.Fprintf(w, "  Auto     %s transport=%s certified=%v bridge=%v\n",
		auto.Mode, auto.Transport, auto.WSSCertified, auto.WSSBridgeAvailable)
	if auto.CurrentCodex != "" || auto.CurrentSlimference != "" || auto.CertifiedCodex != "" || auto.CertifiedSlimference != "" {
		fmt.Fprintf(w, "           current codex=%s slimference=%s\n", auto.CurrentCodex, auto.CurrentSlimference)
		if auto.CertifiedCodex != "" || auto.CertifiedSlimference != "" {
			fmt.Fprintf(w, "           certified codex=%s slimference=%s\n", auto.CertifiedCodex, auto.CertifiedSlimference)
		}
	}
	if auto.FallbackReason != "" {
		fmt.Fprintf(w, "           %s\n", auto.FallbackReason)
	}
	if auto.NeedsRecert {
		fmt.Fprintf(w, "           WSS savings repair needed; recert action: %s\n", auto.RecertCommand)
	}
	if auto.RecertStatus != "" {
		fmt.Fprintf(w, "           recert status=%s attempt=%s\n", auto.RecertStatus, auto.RecertAttemptID)
	}
	fmt.Fprintf(w, "  Daemon   reachable=%v\n", daemonReachable)
	if daemonErr != "" {
		fmt.Fprintf(w, "           %s\n", daemonErr)
	}
	if s.Conflict != "" {
		fmt.Fprintf(w, "  Conflict %s\n", s.Conflict)
	}
	if s.LegacyKeys {
		fmt.Fprintln(w, "  Legacy   openai_base_url/chatgpt_base_url present outside scoped route")
	}
	fmt.Fprintln(w)
	if s.Complete && daemonReachable {
		fmt.Fprintln(w, "Codex CLI/App route is ready after Codex reload.")
	} else if s.Enabled && !daemonReachable {
		fmt.Fprintln(w, "Route is configured but daemon is unreachable. Run `slimference disable` for direct fallback.")
	} else {
		fmt.Fprintln(w, "Route is disabled. Use `slimference codex run -- <prompt>` for one-shot scoped CLI, or `slimference enable` for CLI/App.")
	}
}

const codexUsageLine = "usage: slimference codex <run|enable|disable|status|certify|recertify|desktop|launch-desktop> [flags]\n"

const codexHelpText = `usage: slimference codex <run|enable|disable|status|certify|recertify|desktop|launch-desktop> [flags]

Codex-scoped routing. This is the product path: it only touches Codex
CLI / Codex Desktop App config and never routes Browser ChatGPT,
ChatGPT.app, Claude Code, or generic OpenAI tools through Slimference.

Commands:
  run             run one Codex CLI process through Slimference; fail-open direct
  enable          persist the shared Codex CLI/App provider route
  disable         remove the shared Codex CLI/App provider route
  status          show route config + daemon health
  certify         issue local WSS auto-promotion proof after live mutation
  recertify       run guided WSS repair; refresh Phase-F cert or bridge proof
  desktop         show Desktop app-server shim readiness and live-proof status
  launch-desktop  spawn Codex.app with process-local Slimference env (--probe to inspect)
`

const codexRunHelpText = `usage: slimference codex run [--transport=http|wss|wss-bridge|direct|auto] [--direct] [--host=127.0.0.1] [--port=8990] [-- <codex-args>...]

Runs one Codex CLI process. Default mode health-checks Slimference and
uses the scoped provider override. If the daemon is unreachable it
falls back to direct Codex automatically.

Transport:
  http    stable scoped Responses path, WebSockets disabled
  wss     scoped Responses WebSocket path with Phase-F frame mutation
  wss-bridge scoped Responses WebSocket path with byte-equal frame bridge
  auto    WSS-first ladder: wss_phasef -> wss_bridge -> http -> direct
  direct  no Slimference route
`

const codexEnableHelpText = `usage: slimference enable [--transport=auto|http|wss] [--host=127.0.0.1] [--port=8990] [--dry-run]
alias: slimference codex enable [same flags]

Writes a marker-owned provider block into ~/.codex/config.toml:
model_provider="slimference-codex", base_url=http://127.0.0.1:8990/backend-api/codex,
requires_openai_auth=true, supports_websockets=<transport dependent>, wire_api="responses".

This is scoped to Codex. It does not touch /etc/hosts, pf, system proxy,
Browser ChatGPT, ChatGPT.app, Claude Code, or generic OpenAI clients.
`

const codexDisableHelpText = `usage: slimference disable [--dry-run]
alias: slimference codex disable [same flags]

Removes the marker-owned provider block from ~/.codex/config.toml.
`

const codexStatusHelpText = `usage: slimference codex status [--json] [--host=127.0.0.1] [--port=8990]

Shows whether the scoped Codex provider route is configured and whether
the Slimference daemon is reachable.
`

const codexDesktopUsageLine = "usage: slimference codex desktop <status|prove> [flags]\n"

const codexDesktopHelpText = `usage: slimference codex desktop <status|prove> [flags]

Desktop-specific scoped app-server shim readiness and proof.

Commands:
  status  show CA, daemon, and WSS proof state without launching Codex.app
  prove   launch or finish a process-local Desktop WSS proof
`

const codexDesktopStatusHelpText = `usage: slimference codex desktop status [--json] [--host=127.0.0.1] [--port=8990]

Reports whether the process-local CODEX_CLI_PATH app-server shim is ready
and whether a live Desktop conversation WSS proof has already been seen.
`

const codexDesktopProveHelpText = `usage: slimference codex desktop prove [--json] [--duration=15s] [--manual|--finish] [--keep-open] [--host=127.0.0.1] [--port=8990]

Starts Codex.app with process-local CODEX_CLI_PATH app-server shim, snapshots daemon WSS
counters before/after, classifies the result, and closes the spawned app
unless --keep-open is set. Exit 0 means Desktop Phase-F savings were actually
proven, or --manual produced a launch-ready proof session that still needs a
prompt plus --finish. Exit 1 means the proof was not green and the output names
the failure class.

Use --manual to start a prompt-driven proof session and keep the launched app
open when it is ready. Send a prompt in that app, then run --finish to compare
the current daemon WSS state against the saved session baseline.
`

const codexCertifyHelpText = `usage: slimference codex certify wss [--dry-run] [--operator NAME] [--notes TEXT] [--host=127.0.0.1] [--port=8990]

Writes ~/.slimference/codex-wss-cert.json only when the live daemon has
already observed scoped Codex WSS Phase-F mutation with zero parser,
degradation, or compression errors. The proof is local and version-bound;
auto starts recert repair after Codex or Slimference version drift and uses
WSS bridge before HTTP when bridge proof is available.
`

const codexRecertifyHelpText = `usage: slimference codex recertify wss [--dry-run] [--no-write] [--force] [--json] [--operator NAME] [--notes TEXT] [--timeout=180s] [--host=127.0.0.1] [--port=8990]

Runs the guided Codex CLI WSS repair sequence. A green Phase-F proof writes
~/.slimference/codex-wss-cert.json and restores max-savings auto=WSS. If
Phase-F mutation does not fire but WSS bytes/frames are clean, the command
writes ~/.slimference/codex-wss-bridge.json so auto can keep native WSS
instead of falling straight to HTTP.
`
