package filter

import (
	"strings"

	"github.com/Christopher-Schulze/Slimference/internal/compression"
)

// CompactCapturedOutput applies deterministic Layer-0 compaction to already captured
// tool output. It is used by PostToolUse hooks where the command has already run.
// Returns the compacted output and whether it materially changed.
func CompactCapturedOutput(workDir, commandLine, output string, maxRunes int) ([]byte, bool) {
	return CompactCapturedOutputWithContext(workDir, commandLine, output, maxRunes, FileReadContext{Mode: "scan"})
}

// CompactCapturedOutputWithContext applies Layer-0 compaction with file-read
// safety context from hook/session state.
func CompactCapturedOutputWithContext(workDir, commandLine, output string, maxRunes int, ctx FileReadContext) ([]byte, bool) {
	stripped := compression.StripANSICodes(output)
	argv := primaryArgvForCapturedOutput(commandLine)
	if len(argv) == 0 {
		if normalized := NormalizePathListCommandLine(commandLine, workDir); normalized != "" {
			argv = primaryArgvForCapturedOutput(normalized)
		} else if spec, ok := searchCommandSpecFromCommandLine(commandLine, workDir, false); ok {
			argv = spec.argv
		}
	}

	compacted := []byte(stripped)
	if len(argv) == 0 {
		compacted = TruncateStdoutWithHint(compacted, maxRunes)
		return compacted, strings.TrimSpace(string(compacted)) != strings.TrimSpace(stripped)
	}

	compacted = applyLayer0AfterANSIWithContext(workDir, argv, compacted, ctx)
	compacted = TruncateStdoutWithHint(compacted, maxRunes)
	if strings.TrimSpace(string(compacted)) == strings.TrimSpace(stripped) {
		return compacted, false
	}
	return compacted, true
}

func ArgvForCapturedOutput(commandLine string) []string {
	return primaryArgvForCapturedOutput(commandLine)
}

func primaryArgvForCapturedOutput(commandLine string) []string {
	toks := tokenize(commandLine)
	if len(toks) == 0 {
		return nil
	}

	argv := make([]string, 0, 8)
	for _, tok := range toks {
		switch tok.Kind {
		case TokenOperator, TokenPipe, TokenRedirect, TokenShellism:
			return nil
		case TokenArg:
			if len(argv) == 0 && isEnvAssignmentToken(tok.Value) {
				continue
			}
			argv = append(argv, tok.Value)
		}
	}
	if len(argv) == 0 {
		return nil
	}
	return argv
}

func isEnvAssignmentToken(token string) bool {
	idx := strings.Index(token, "=")
	if idx <= 0 {
		return false
	}
	name := token[:idx]
	return !strings.ContainsAny(name, " /.-")
}
