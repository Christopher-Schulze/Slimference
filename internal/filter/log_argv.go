package filter

import (
	"path/filepath"
	"strings"
)

var logReadingBases = map[string]bool{
	"tail": true, "head": true, "less": true, "more": true,
	"cat": true, "journalctl": true, "lnav": true, "multitail": true,
}

var logReadingPrefixes = map[string]bool{
	"tail": true, "head": true, "less": true, "more": true,
	"cat": true, "journalctl": true, "lnav": true, "multitail": true,
	"grep": true, "awk": true, "sed": true, "rg": true,
}

var logFileExtensions = []string{".log", ".txt", ".out", ".err"}

func isLogReadingArgv(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	base := strings.ToLower(filepath.Base(strings.TrimPrefix(argv[0], "./")))

	if logReadingBases[base] && len(argv) >= 2 {
		return true
	}
	if isDockerLogsArgv(argv) || isKubectlLogsArgv(argv) {
		return true
	}

	if logReadingPrefixes[base] && len(argv) >= 2 {
		last := argv[len(argv)-1]
		ext := strings.ToLower(filepath.Ext(last))
		for _, e := range logFileExtensions {
			if ext == e {
				return true
			}
		}
	}
	return false
}
