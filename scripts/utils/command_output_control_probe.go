package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type commandOutputProbeFlags struct {
	jsonOut      bool
	keepDir      bool
	workDir      string
	logPath      string
	realShell    string
	timeout      time.Duration
	shimCommands []string
	childArgs    []string
}

type commandOutputProbeResult struct {
	StartedAt       string                            `json:"started_at"`
	EndedAt         string                            `json:"ended_at"`
	WorkDir         string                            `json:"work_dir,omitempty"`
	ProbeDir        string                            `json:"probe_dir"`
	LogPath         string                            `json:"log_path"`
	ChildCommand    string                            `json:"child_command,omitempty"`
	ChildExitCode   int                               `json:"child_exit_code"`
	TimedOut        bool                              `json:"timed_out"`
	Observed        commandOutputProbeObserved        `json:"observed"`
	ShimCommands    map[string]commandOutputProbeShim `json:"shim_commands,omitempty"`
	GeneratedEnv    commandOutputProbeGeneratedEnv    `json:"generated_env"`
	RouteSafety     commandOutputProbeRouteSafety     `json:"route_safety"`
	Findings        []string                          `json:"findings"`
	ActivationBlock string                            `json:"activation_block"`
}

type commandOutputProbeObserved struct {
	ShellWrapper bool            `json:"shell_wrapper"`
	BashEnv      bool            `json:"bash_env"`
	PathShims    map[string]bool `json:"path_shims,omitempty"`
}

type commandOutputProbeShim struct {
	RealPath string `json:"real_path,omitempty"`
	Created  bool   `json:"created"`
	Reason   string `json:"reason,omitempty"`
}

type commandOutputProbeGeneratedEnv struct {
	Shell      string `json:"SHELL"`
	BashEnv    string `json:"BASH_ENV"`
	PathPrefix string `json:"PATH_prefix"`
	LogPath    string `json:"SLIMFERENCE_T418_PROBE_LOG"`
}

type commandOutputProbeRouteSafety struct {
	ProcessLocalOnly       bool `json:"process_local_only"`
	WritesShellRC          bool `json:"writes_shell_rc"`
	WritesCodexConfig      bool `json:"writes_codex_config"`
	WritesGlobalProxy      bool `json:"writes_global_proxy"`
	TouchesHostsOrPFCTL    bool `json:"touches_hosts_or_pfctl"`
	TouchesNormalCodexApps bool `json:"touches_normal_codex_apps"`
}

type commandOutputProbeEvent struct {
	Seam    string
	Command string
}

type repeatedStringFlag []string

func (f *repeatedStringFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *repeatedStringFlag) Set(v string) error {
	v = strings.TrimSpace(v)
	if v == "" {
		return fmt.Errorf("empty shim command")
	}
	*f = append(*f, v)
	return nil
}

func runCommandOutputControlProbe(args []string, stdout, stderr io.Writer) int {
	flags, err := parseCommandOutputProbeFlags(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	result, err := commandOutputControlProbe(flags, stdout, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if flags.jsonOut {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			fmt.Fprintf(stderr, "encode result: %v\n", err)
			return 1
		}
	} else {
		printCommandOutputProbeText(stdout, result)
	}
	if result.ChildExitCode != 0 {
		return result.ChildExitCode
	}
	return 0
}

func parseCommandOutputProbeFlags(args []string) (commandOutputProbeFlags, error) {
	var shims repeatedStringFlag
	flags := commandOutputProbeFlags{}
	fs := flag.NewFlagSet("command-output-control-probe", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&flags.jsonOut, "json", false, "emit JSON")
	fs.BoolVar(&flags.keepDir, "keep-dir", false, "keep the generated probe directory")
	fs.StringVar(&flags.workDir, "workdir", "", "child working directory")
	fs.StringVar(&flags.logPath, "log", "", "probe log path")
	fs.StringVar(&flags.realShell, "real-shell", "", "real shell used by the probe shell wrapper")
	fs.DurationVar(&flags.timeout, "timeout", 2*time.Minute, "child command timeout")
	fs.Var(&shims, "shim-command", "command name to place a process-local PATH shim for; repeatable")
	if err := fs.Parse(args); err != nil {
		return flags, err
	}
	rest := fs.Args()
	if len(rest) > 0 && rest[0] == "--" {
		rest = rest[1:]
	}
	if len(rest) == 0 {
		return flags, fmt.Errorf("usage: command-output-control-probe [--json] [--shim-command=name] -- <command> [args...]")
	}
	flags.shimCommands = normalizeProbeShimCommands(shims)
	if len(flags.shimCommands) == 0 {
		flags.shimCommands = []string{"git", "rg", "grep", "go", "cargo", "npm", "python3"}
	}
	flags.childArgs = rest
	return flags, nil
}

func commandOutputControlProbe(flags commandOutputProbeFlags, stdout, stderr io.Writer) (commandOutputProbeResult, error) {
	started := time.Now().UTC()
	probeDir, err := os.MkdirTemp("", "slimference-t418-probe-*")
	if err != nil {
		return commandOutputProbeResult{}, err
	}
	if !flags.keepDir {
		defer os.RemoveAll(probeDir)
	}
	logPath := flags.logPath
	if logPath == "" {
		logPath = filepath.Join(probeDir, "events.log")
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return commandOutputProbeResult{}, err
	}
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		return commandOutputProbeResult{}, err
	}

	realShell := flags.realShell
	if realShell == "" {
		realShell = os.Getenv("SHELL")
	}
	if realShell == "" {
		realShell = "/bin/sh"
	}
	shellWrapper := filepath.Join(probeDir, "slimference-probe-shell")
	bashEnv := filepath.Join(probeDir, "bash_env")
	pathDir := filepath.Join(probeDir, "path")
	if err := os.MkdirAll(pathDir, 0o755); err != nil {
		return commandOutputProbeResult{}, err
	}
	if err := writeCommandOutputProbeShellWrapper(shellWrapper); err != nil {
		return commandOutputProbeResult{}, err
	}
	if err := writeCommandOutputProbeBashEnv(bashEnv); err != nil {
		return commandOutputProbeResult{}, err
	}

	shims := make(map[string]commandOutputProbeShim)
	for _, name := range flags.shimCommands {
		realPath, lookErr := exec.LookPath(name)
		if lookErr != nil {
			shims[name] = commandOutputProbeShim{Created: false, Reason: "real_command_not_found"}
			continue
		}
		if err := writeCommandOutputProbePathShim(filepath.Join(pathDir, name), name, realPath); err != nil {
			return commandOutputProbeResult{}, err
		}
		shims[name] = commandOutputProbeShim{RealPath: realPath, Created: true}
	}

	childEnv := commandOutputProbeEnv(os.Environ(), map[string]string{
		"SHELL":                         shellWrapper,
		"BASH_ENV":                      bashEnv,
		"SLIMFERENCE_T418_PROBE_LOG":    logPath,
		"SLIMFERENCE_T418_REAL_SHELL":   realShell,
		"SLIMFERENCE_T418_PATH_PREFIX":  pathDir,
		"SLIMFERENCE_T418_PROBE_ACTIVE": "1",
		"PATH":                          pathDir + string(os.PathListSeparator) + os.Getenv("PATH"),
	})

	ctx := context.Background()
	cancel := func() {}
	if flags.timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, flags.timeout)
	}
	defer cancel()
	cmd := exec.CommandContext(ctx, flags.childArgs[0], flags.childArgs[1:]...)
	cmd.Env = childEnv
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if flags.workDir != "" {
		cmd.Dir = flags.workDir
	}
	runErr := cmd.Run()
	timedOut := ctx.Err() == context.DeadlineExceeded
	exitCode := commandOutputProbeExitCode(runErr)
	events, readErr := readCommandOutputProbeEvents(logPath)
	if readErr != nil {
		return commandOutputProbeResult{}, readErr
	}

	result := commandOutputProbeResult{
		StartedAt:     started.Format(time.RFC3339Nano),
		EndedAt:       time.Now().UTC().Format(time.RFC3339Nano),
		WorkDir:       flags.workDir,
		ProbeDir:      probeDir,
		LogPath:       logPath,
		ChildCommand:  filepath.Base(flags.childArgs[0]),
		ChildExitCode: exitCode,
		TimedOut:      timedOut,
		Observed: commandOutputProbeObserved{
			PathShims: map[string]bool{},
		},
		ShimCommands: shims,
		GeneratedEnv: commandOutputProbeGeneratedEnv{
			Shell:      shellWrapper,
			BashEnv:    bashEnv,
			PathPrefix: pathDir,
			LogPath:    logPath,
		},
		RouteSafety: commandOutputProbeRouteSafety{
			ProcessLocalOnly:       true,
			WritesShellRC:          false,
			WritesCodexConfig:      false,
			WritesGlobalProxy:      false,
			TouchesHostsOrPFCTL:    false,
			TouchesNormalCodexApps: false,
		},
		ActivationBlock: "This probe only proves process-local control-point visibility for the child command. Product command-output-first activation still requires a scoped Codex live proof, byte-equal fail-open tests, recovery accounting, and route hygiene.",
	}
	for name := range shims {
		result.Observed.PathShims[name] = false
	}
	for _, event := range events {
		switch event.Seam {
		case "shell":
			result.Observed.ShellWrapper = true
		case "bash_env":
			result.Observed.BashEnv = true
		case "path_shim":
			if event.Command != "" {
				result.Observed.PathShims[event.Command] = true
			}
		}
	}
	result.Findings = commandOutputProbeFindings(result)
	if timedOut {
		result.Findings = append(result.Findings, "child_timeout")
	}
	if runErr != nil && exitCode == 1 && !timedOut {
		var exitErr *exec.ExitError
		if !errors.As(runErr, &exitErr) {
			return result, runErr
		}
	}
	return result, nil
}

func writeCommandOutputProbeShellWrapper(path string) error {
	const body = `#!/bin/sh
log="${SLIMFERENCE_T418_PROBE_LOG:-}"
if [ -n "$log" ]; then
  printf 'shell\tcommand=shell\targc=%s\n' "$#" >> "$log"
fi
real="${SLIMFERENCE_T418_REAL_SHELL:-/bin/sh}"
exec "$real" "$@"
`
	return writeExecutableProbeFile(path, body)
}

func writeCommandOutputProbeBashEnv(path string) error {
	const body = `log="${SLIMFERENCE_T418_PROBE_LOG:-}"
if [ -n "$log" ]; then
  printf 'bash_env\tcommand=bash\targc=0\n' >> "$log"
fi
`
	return os.WriteFile(path, []byte(body), 0o600)
}

func writeCommandOutputProbePathShim(path, name, realPath string) error {
	body := "#!/bin/sh\n" +
		"log=\"${SLIMFERENCE_T418_PROBE_LOG:-}\"\n" +
		"if [ -n \"$log\" ]; then\n" +
		"  printf 'path_shim\\tcommand=" + shellSingleQuoteForProbe(name) + "\\targc=%s\\n' \"$#\" >> \"$log\"\n" +
		"fi\n" +
		"exec " + shellSingleQuoteForProbe(realPath) + " \"$@\"\n"
	return writeExecutableProbeFile(path, body)
}

func writeExecutableProbeFile(path, body string) error {
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		return err
	}
	return nil
}

func commandOutputProbeEnv(base []string, overrides map[string]string) []string {
	out := make([]string, 0, len(base)+len(overrides))
	seen := map[string]bool{}
	for _, kv := range base {
		key, _, ok := strings.Cut(kv, "=")
		if !ok || overrides[key] != "" || key == "PATH" {
			continue
		}
		seen[key] = true
		out = append(out, kv)
	}
	keys := make([]string, 0, len(overrides))
	for key := range overrides {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if key == "" {
			continue
		}
		seen[key] = true
		out = append(out, key+"="+overrides[key])
	}
	_ = seen
	return out
}

func readCommandOutputProbeEvents(path string) ([]commandOutputProbeEvent, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var events []commandOutputProbeEvent
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		event := commandOutputProbeEvent{}
		parts := strings.Split(line, "\t")
		event.Seam = parts[0]
		for _, part := range parts[1:] {
			key, value, ok := strings.Cut(part, "=")
			if ok && key == "command" {
				event.Command = value
			}
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

func commandOutputProbeExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return 1
}

func commandOutputProbeFindings(result commandOutputProbeResult) []string {
	var findings []string
	if result.Observed.ShellWrapper {
		findings = append(findings, "shell_wrapper_observed")
	} else {
		findings = append(findings, "shell_wrapper_not_observed")
	}
	if result.Observed.BashEnv {
		findings = append(findings, "bash_env_observed")
	} else {
		findings = append(findings, "bash_env_not_observed")
	}
	var shimNames []string
	for name := range result.Observed.PathShims {
		shimNames = append(shimNames, name)
	}
	sort.Strings(shimNames)
	for _, name := range shimNames {
		if result.Observed.PathShims[name] {
			findings = append(findings, "path_shim_observed:"+name)
		}
	}
	if len(findings) == 0 {
		findings = append(findings, "no_control_point_observed")
	}
	return findings
}

func printCommandOutputProbeText(w io.Writer, result commandOutputProbeResult) {
	fmt.Fprintf(w, "Command-output control probe\n")
	fmt.Fprintf(w, "child=%s exit=%d\n", result.ChildCommand, result.ChildExitCode)
	fmt.Fprintf(w, "shell_wrapper=%v bash_env=%v\n", result.Observed.ShellWrapper, result.Observed.BashEnv)
	var shimNames []string
	for name := range result.Observed.PathShims {
		shimNames = append(shimNames, name)
	}
	sort.Strings(shimNames)
	for _, name := range shimNames {
		fmt.Fprintf(w, "path_shim[%s]=%v\n", name, result.Observed.PathShims[name])
	}
	fmt.Fprintf(w, "process_local_only=%v\n", result.RouteSafety.ProcessLocalOnly)
	for _, finding := range result.Findings {
		fmt.Fprintf(w, "finding=%s\n", finding)
	}
}

func normalizeProbeShimCommands(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, raw := range in {
		raw = strings.TrimSpace(raw)
		if strings.ContainsAny(raw, `/\`) {
			continue
		}
		name := filepath.Base(raw)
		if name == "" || name == "." || name == string(os.PathSeparator) {
			continue
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func shellSingleQuoteForProbe(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
