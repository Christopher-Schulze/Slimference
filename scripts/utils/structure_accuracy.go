package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Christopher-Schulze/Slimference/internal/compression"
)

// T35: structure-extract accuracy measurement scaffolding.
//
// Layer 1.4 uses regex-based extractors (tree-sitter was dropped to stay
// CGO-free). We need a measurable picture of how often the extractor:
//   - fails to recognise a declaration that is present (false negative)
//   - emits content that is missing in the input (false positive)
//
// This harness reads a corpus under a root directory and, for each
// supported source file, runs ExtractStructure and reports:
//   - original length
//   - summary length (or "not changed" when the extractor declined)
//   - whether any declaration tokens from the input survived in the
//     summary (a minimal precision check)
//
// Ground truth parsers per language are out of scope for the MVP (see
// T35 plan). The output is intentionally a delta-able report so an
// operator can expand the corpus and track drift over time.

type structureAccuracyRow struct {
	Path       string
	Language   string
	InputLen   int
	SummaryLen int
	Changed    bool
	SurvivedN  int
	TotalDecls int
}

var languageByExt = map[string]string{
	".go":   "go",
	".py":   "python",
	".rs":   "rust",
	".ts":   "typescript",
	".tsx":  "typescript",
	".js":   "javascript",
	".jsx":  "javascript",
	".rb":   "ruby",
	".java": "java",
	".c":    "c",
	".h":    "c",
	".cpp":  "cpp",
	".hpp":  "cpp",
	".sh":   "shell",
}

// measureStructureAccuracyDir walks root and returns one row per supported
// file. Non-source files are skipped silently.
func measureStructureAccuracyDir(root string) ([]structureAccuracyRow, error) {
	var rows []structureAccuracyRow
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		lang, ok := languageByExt[strings.ToLower(filepath.Ext(path))]
		if !ok {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		row := measureStructureAccuracy(path, lang, string(data))
		rows = append(rows, row)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// measureStructureAccuracy is the per-file probe reused by tests.
func measureStructureAccuracy(path, lang, content string) structureAccuracyRow {
	summary, changed := compression.ExtractStructure(content, lang)
	survived, total := countSurvivedDeclarations(content, summary, lang)
	return structureAccuracyRow{
		Path:       path,
		Language:   lang,
		InputLen:   len(content),
		SummaryLen: len(summary),
		Changed:    changed,
		SurvivedN:  survived,
		TotalDecls: total,
	}
}

// countSurvivedDeclarations does a trivial overlap check: how many of
// the input's top-level declaration tokens (function / class / type
// keywords followed by an identifier) still appear in the summary.
// It is a minimal signal - a full precision/recall measurement needs a
// real parser per language (T35 phase 2).
func countSurvivedDeclarations(original, summary, lang string) (survived, total int) {
	var keywords []string
	switch lang {
	case "go":
		keywords = []string{"func ", "type ", "const ", "var "}
	case "python":
		keywords = []string{"def ", "class ", "async def "}
	case "rust":
		keywords = []string{"fn ", "struct ", "enum ", "impl ", "trait "}
	case "typescript", "javascript":
		keywords = []string{"function ", "class ", "const ", "interface ", "type "}
	case "ruby":
		keywords = []string{"def ", "class ", "module "}
	case "java":
		keywords = []string{"class ", "interface ", "enum "}
	default:
		keywords = []string{"function ", "def ", "class ", "fn "}
	}
	for _, line := range strings.Split(original, "\n") {
		trimmed := strings.TrimSpace(line)
		for _, kw := range keywords {
			if strings.HasPrefix(trimmed, kw) {
				total++
				if strings.Contains(summary, trimmed) {
					survived++
				}
				break
			}
		}
	}
	return survived, total
}

// formatStructureAccuracyReport renders the rows as a human-readable table.
func formatStructureAccuracyReport(rows []structureAccuracyRow) string {
	if len(rows) == 0 {
		return "No source files found in corpus.\n"
	}
	var sb strings.Builder
	var totalIn, totalOut int
	var totalDecls, totalSurvived int
	changedCount := 0
	sb.WriteString(fmt.Sprintf("%-50s %-10s %7s %7s %7s %-9s %s\n", "path", "lang", "in", "out", "ratio", "decls", "note"))
	sb.WriteString(strings.Repeat("-", 120))
	sb.WriteString("\n")
	for _, r := range rows {
		ratio := 1.0
		if r.InputLen > 0 {
			ratio = float64(r.SummaryLen) / float64(r.InputLen)
		}
		note := "unchanged"
		if r.Changed {
			note = "structured"
			changedCount++
		}
		declsCol := fmt.Sprintf("%d/%d", r.SurvivedN, r.TotalDecls)
		sb.WriteString(fmt.Sprintf("%-50s %-10s %7d %7d %7.2f %-9s %s\n",
			truncateLeft(r.Path, 50), r.Language, r.InputLen, r.SummaryLen, ratio, declsCol, note))
		totalIn += r.InputLen
		totalOut += r.SummaryLen
		totalDecls += r.TotalDecls
		totalSurvived += r.SurvivedN
	}
	sb.WriteString(strings.Repeat("-", 120))
	sb.WriteString("\n")
	ratio := 1.0
	if totalIn > 0 {
		ratio = float64(totalOut) / float64(totalIn)
	}
	recall := 0.0
	if totalDecls > 0 {
		recall = float64(totalSurvived) / float64(totalDecls)
	}
	sb.WriteString(fmt.Sprintf("Files: %d  changed: %d  size_ratio: %.2f  decl_recall: %.2f (%d/%d)\n",
		len(rows), changedCount, ratio, recall, totalSurvived, totalDecls))
	return sb.String()
}

// truncateLeft crops s to fit width chars by keeping the tail and
// prefixing with an ellipsis when truncation was needed.
func truncateLeft(s string, width int) string {
	if len(s) <= width {
		return s
	}
	return "..." + s[len(s)-(width-3):]
}

// runStructureAccuracy is the subcommand entry point wired into main.go.
func runStructureAccuracy(args []string, out, errOut io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(errOut, "Usage: go run ./scripts/utils structure-accuracy <corpus-dir>")
		return 2
	}
	info, err := os.Stat(args[0])
	if err != nil {
		fmt.Fprintf(errOut, "structure-accuracy: %v\n", err)
		return 1
	}
	if !info.IsDir() {
		fmt.Fprintln(errOut, "structure-accuracy: argument must be a directory")
		return 1
	}
	rows, err := measureStructureAccuracyDir(args[0])
	if err != nil {
		fmt.Fprintf(errOut, "structure-accuracy: %v\n", err)
		return 1
	}
	// Write via buffered writer so the output is atomic per call.
	w := bufio.NewWriter(out)
	defer w.Flush()
	_, _ = io.WriteString(w, formatStructureAccuracyReport(rows))
	return 0
}
