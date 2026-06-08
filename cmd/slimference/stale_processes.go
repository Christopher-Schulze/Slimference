package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

type staleSlimferenceProcess struct {
	PID  int
	Stat string
	Args string
}

var (
	psSlimferenceProcessesFn                   = listStaleSlimferenceProcessesViaPS
	staleSlimferenceProcessNoticeFn            = staleSlimferenceProcessNotice
	staleSlimferenceProcessNoticeIgnoringPIDFn = staleSlimferenceProcessNoticeIgnoringPID
	staleSlimferenceProcessSelfPIDFn           = os.Getpid
	staleSlimferenceProcessCommandFn           = func(name string, args ...string) *exec.Cmd { return exec.Command(name, args...) }
)

func staleSlimferenceProcessNotice() string {
	return staleSlimferenceProcessNoticeIgnoringPID(0)
}

func staleSlimferenceProcessNoticeIgnoringPID(ignorePID int) string {
	procs, err := psSlimferenceProcessesFn()
	if err != nil || len(procs) == 0 {
		return ""
	}
	if ignorePID > 0 {
		filtered := procs[:0]
		for _, proc := range procs {
			if proc.PID != ignorePID {
				filtered = append(filtered, proc)
			}
		}
		procs = filtered
	}
	return formatStaleSlimferenceProcessNotice(procs)
}

func listStaleSlimferenceProcessesViaPS() ([]staleSlimferenceProcess, error) {
	cmd := staleSlimferenceProcessCommandFn("ps", "-axo", "pid=,stat=,args=")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return parseStaleSlimferenceProcesses(string(out), staleSlimferenceProcessSelfPIDFn()), nil
}

func parseStaleSlimferenceProcesses(output string, selfPID int) []staleSlimferenceProcess {
	var out []staleSlimferenceProcess
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil || pid == selfPID {
			continue
		}
		stat := fields[1]
		args := strings.Join(fields[2:], " ")
		if !isSlimferenceProcessArgs(args) {
			continue
		}
		if !staleSlimferenceProcessLooksStuck(stat, args) {
			continue
		}
		out = append(out, staleSlimferenceProcess{PID: pid, Stat: stat, Args: args})
	}
	return out
}

func isSlimferenceProcessArgs(args string) bool {
	first, _, _ := strings.Cut(strings.TrimSpace(args), " ")
	if first == "" {
		return false
	}
	base := first
	if idx := strings.LastIndex(base, "/"); idx >= 0 {
		base = base[idx+1:]
	}
	return strings.HasPrefix(base, "slimference")
}

func staleSlimferenceProcessLooksStuck(stat string, args string) bool {
	if strings.Contains(stat, "U") {
		return true
	}
	return strings.Contains(args, "dyld_start") || strings.Contains(args, ".dyld-stuck-")
}

func formatStaleSlimferenceProcessNotice(procs []staleSlimferenceProcess) string {
	if len(procs) == 0 {
		return ""
	}
	pids := make([]string, 0, len(procs))
	for _, proc := range procs {
		pids = append(pids, fmt.Sprintf("%d(%s)", proc.PID, proc.Stat))
	}
	return fmt.Sprintf("%d old stuck Slimference process(es): %s. Current daemon may still be healthy; macOS reboot clears U/UE/dyld_start state, retry-looping stop will not.", len(procs), strings.Join(pids, ", "))
}
