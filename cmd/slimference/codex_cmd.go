package main

import (
	"bytes"
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
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/codexroute"
	"github.com/Christopher-Schulze/Slimference/internal/control"
	"github.com/Christopher-Schulze/Slimference/internal/proxy"
	"github.com/Christopher-Schulze/Slimference/internal/tlsca"
	"github.com/Christopher-Schulze/Slimference/internal/transparent"
)

var (
	codexRouteHomeFn         = os.UserHomeDir
	codexRouteEnableFn       = codexroute.EnableWithOptions
	codexRouteDisableFn      = codexroute.Disable
	codexRouteInspectFn      = codexroute.InspectWithOptions
	codexRouteHealthFn       = defaultProxyHealthCheck
	codexProxyRunFn          = proxyRun
	codexVersionFn           = currentCodexVersion
	codexAutoFn              = resolveCodexAutoTransport
	codexCertSaveFn          = codexroute.SaveCertification
	codexBridgeSaveFn        = codexroute.SaveBridgeProof
	codexRecertSaveFn        = codexroute.SaveRecertState
	codexAutoRecertFn        = startCodexAutoRecert
	codexRecertTriggerFn     = defaultCodexRecertTrigger
	codexRecertLogFn         = appendCodexRecertLog
	codexSetupStateFn        = fetchCodexSetupState
	codexVersionOutFn        = defaultCodexCLIVersionOutput
	codexNowFn               = time.Now
	codexDesktopCleanupFn    = cleanupCodexDesktopProcess
	codexDesktopSessionFn    = codexDesktopProofSessionPath
	codexDesktopResultFn     = codexDesktopProofResultPath
	codexDesktopWSSCaptureFn = setCodexDesktopWSSCapture
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
	restoreTitle := func() {}
	if mode != "direct" {
		restoreTitle = setScopedCodexTerminalTitle(codexArgs)
		defer restoreTitle()
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
		fmt.Fprintf(p.Out, "Dry-run: would write advanced shared Codex route to %s\n\n%s",
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
	fmt.Fprintf(p.Out, "Advanced shared Codex route enabled: %s (%s)\n", evt.Path, evt.Action)
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
		fmt.Fprintf(p.Out, "Dry-run: would remove advanced shared Codex route from %s\n", codexroute.ConfigPath(home))
		return 0
	}
	evt, err := codexRouteDisableFn(home)
	if err != nil {
		fmt.Fprintf(p.Err, "codex disable: %v\n", err)
		return 1
	}
	fmt.Fprintf(p.Out, "Normal Codex direct. Advanced shared route off: %s (%s)\n", evt.Path, evt.Action)
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

func setCodexDesktopWSSCapture(host, port, path string, enabled bool, timeout time.Duration) error {
	addr := net.JoinHostPort(host, port)
	payload := proxy.AdminWSSCaptureRequest{
		Enabled: enabled,
		Path:    path,
	}
	if enabled {
		payload.DurationSeconds = int((6 * time.Hour).Seconds())
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+addr+proxy.AdminWSSCapturePath, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("admin returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
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
	fmt.Fprintln(w, "Slimference Codex traffic")
	fmt.Fprintln(w, "-------------------------")
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
		if auto.BridgeCodex != "" || auto.BridgeSlimference != "" {
			fmt.Fprintf(w, "           bridge codex=%s slimference=%s\n", auto.BridgeCodex, auto.BridgeSlimference)
		}
	}
	if auto.FallbackReason != "" {
		fmt.Fprintf(w, "           %s\n", auto.FallbackReason)
	}
	if auto.NeedsRecert {
		if auto.WSSBridgeAvailable {
			fmt.Fprintf(w, "           Phase-F savings repair available; bridge is current; recert action: %s\n", auto.RecertCommand)
		} else {
			fmt.Fprintf(w, "           WSS savings repair needed; recert action: %s\n", auto.RecertCommand)
		}
	}
	if auto.RecertStatus != "" {
		fmt.Fprintf(w, "           recert status=%s attempt=%s\n", auto.RecertStatus, auto.RecertAttemptID)
		if !auto.RecertStartedAt.IsZero() {
			fmt.Fprintf(w, "           recert started=%s\n", auto.RecertStartedAt.Format(time.RFC3339))
		}
		if !auto.RecertFinishedAt.IsZero() {
			fmt.Fprintf(w, "           recert finished=%s\n", auto.RecertFinishedAt.Format(time.RFC3339))
		}
		if !auto.RecertLastSuccessAt.IsZero() {
			fmt.Fprintf(w, "           recert last_success=%s\n", auto.RecertLastSuccessAt.Format(time.RFC3339))
		}
		if !auto.RecertRetryAfter.IsZero() {
			fmt.Fprintf(w, "           recert retry_after=%s\n", auto.RecertRetryAfter.Format(time.RFC3339))
		}
		if auto.RecertLastError != "" {
			fmt.Fprintf(w, "           recert last_error=%s\n", auto.RecertLastError)
		}
		if auto.RecertLogPath != "" {
			fmt.Fprintf(w, "           recert log=%s\n", auto.RecertLogPath)
		}
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
		fmt.Fprintln(w, "Advanced shared Codex route is ready after Codex reload.")
	} else if s.Enabled && !daemonReachable {
		fmt.Fprintln(w, "Advanced shared route is configured but daemon is unreachable. Run `slimference disable` for normal direct Codex.")
	} else {
		fmt.Fprintln(w, "Normal Codex direct. Use `slimference codex run -- <prompt>` for one-shot scoped CLI, or `slimference enable` for the advanced shared CLI/App route.")
	}
}

const codexUsageLine = "usage: slimference codex <run|enable|disable|status|certify|recertify|desktop|launch-desktop> [flags]\n"

const codexHelpText = `usage: slimference codex <run|enable|disable|status|certify|recertify|desktop|launch-desktop> [flags]

Codex-scoped routing. This is the product path: it only touches Codex
CLI / Codex Desktop App config and never routes Browser ChatGPT,
ChatGPT.app, Claude Code, or generic OpenAI tools through Slimference.

Commands:
  run             run one Codex CLI process through Slimference; fail-open direct
  enable          persist the advanced shared Codex CLI/App provider route
  disable         remove the advanced shared route; normal Codex stays direct
  status          show normal direct vs advanced shared route state + daemon health
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
  auto    savings-first ladder: wss_phasef -> http -> direct; bridge is diagnostic
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

Shows whether normal Codex is direct or the advanced shared Codex provider
route is configured, plus daemon reachability.
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

const codexDesktopProveHelpText = `usage: slimference codex desktop prove [--json] [--duration=15s] [--manual|--finish] [--keep-open] [--replace-existing] [--capture=<frames.jsonl>] [--matrix-row=<matrix.jsonl>] [--host=127.0.0.1] [--port=8990]

Starts Codex.app with process-local CODEX_CLI_PATH app-server shim, snapshots daemon WSS
counters before/after, classifies the result, and closes the spawned app
unless --keep-open is set. If Codex.app is already running, this command fails
closed unless --replace-existing is passed explicitly. Exit 0 means Desktop
Phase-F savings were actually proven, or --manual produced a launch-ready proof
session that still needs a prompt plus --finish. Exit 1 means the proof was not
green and the output names the failure class.

Use --manual to start a prompt-driven proof session and keep the launched app
open when it is ready. Send a prompt in that app, then run --finish to compare
the current daemon WSS state against the saved session baseline.

Manual proof arms daemon-side WSS A/B capture at a timestamped local capture
path unless --capture is passed. --matrix-row overrides the derived
wss-proof-matrix row path printed by the proof handoff.
`

const codexCertifyHelpText = `usage: slimference codex certify wss [--dry-run] [--operator NAME] [--notes TEXT] [--host=127.0.0.1] [--port=8990]

Writes ~/.slimference/codex-wss-cert.json only when the live daemon has
already observed scoped Codex WSS Phase-F mutation with zero parser,
degradation, or compression errors. The proof is local and version-bound;
auto starts recert repair after Codex or Slimference version drift and uses
HTTP savings fallback while WSS Phase-F is stale. A clean WSS bridge proof is
reported for diagnostics and explicit bridge runs, but auto does not prefer a
byte-equal bridge over HTTP savings.
`

const codexRecertifyHelpText = `usage: slimference codex recertify wss [--dry-run] [--no-write] [--force] [--json] [--operator NAME] [--notes TEXT] [--timeout=180s] [--host=127.0.0.1] [--port=8990]

Runs the guided Codex CLI WSS repair sequence. A green Phase-F proof writes
~/.slimference/codex-wss-cert.json and restores max-savings auto=WSS. If
Phase-F mutation does not fire but WSS bytes/frames are clean, the command
writes ~/.slimference/codex-wss-bridge.json so status can report a safe native
bridge while auto keeps the HTTP savings path until Phase-F recertifies.
`
