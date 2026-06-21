package filter

import (
	"strconv"
	"testing"
)

func TestReadRequestFromArgv_CatFullFile(t *testing.T) {
	t.Parallel()
	req, ok := ReadRequestFromArgv([]string{"cat", "/repo/file.go"})
	if !ok {
		t.Fatal("cat single file must parse")
	}
	if req.Path != "/repo/file.go" || req.Offset != 0 || req.Limit != 0 {
		t.Fatalf("cat request mismatch: %+v", req)
	}
	if !req.IsFull() {
		t.Fatal("cat with no range must be IsFull")
	}
}

func TestReadRequestFromArgv_CatMultipleFilesFails(t *testing.T) {
	t.Parallel()
	if _, ok := ReadRequestFromArgv([]string{"cat", "a.go", "b.go"}); ok {
		t.Fatal("cat with multiple files must not parse")
	}
}

func TestReadRequestFromArgv_HeadWithLimit(t *testing.T) {
	t.Parallel()
	req, ok := ReadRequestFromArgv([]string{"head", "-n", "20", "/repo/file.go"})
	if !ok {
		t.Fatal("head -n 20 must parse")
	}
	if req.Path != "/repo/file.go" || req.Offset != 1 || req.Limit != 20 {
		t.Fatalf("head request mismatch: %+v", req)
	}
}

func TestReadRequestFromArgv_HeadDefaultLimit(t *testing.T) {
	t.Parallel()
	req, ok := ReadRequestFromArgv([]string{"head", "/repo/file.go"})
	if !ok {
		t.Fatal("head without -n must parse with default 10")
	}
	if req.Limit != 10 {
		t.Fatalf("head default limit must be 10, got %d", req.Limit)
	}
}

func TestReadRequestFromArgv_TailWithRange(t *testing.T) {
	t.Parallel()
	req, ok := ReadRequestFromArgv([]string{"tail", "-n", "50", "/repo/file.go"})
	if !ok {
		t.Fatal("tail -n 50 must parse")
	}
	if req.Path != "/repo/file.go" || req.Limit != 50 {
		t.Fatalf("tail request mismatch: %+v", req)
	}
}

func TestReadRequestFromArgv_UnknownCommandFails(t *testing.T) {
	t.Parallel()
	if _, ok := ReadRequestFromArgv([]string{"git", "status"}); ok {
		t.Fatal("git status must not parse as read request")
	}
}

func TestReadRequestFromArgv_EmptyArgvFails(t *testing.T) {
	t.Parallel()
	if _, ok := ReadRequestFromArgv(nil); ok {
		t.Fatal("empty argv must not parse")
	}
}

func TestReadRequestFromArgv_HeadBytesFails(t *testing.T) {
	t.Parallel()
	if _, ok := ReadRequestFromArgv([]string{"head", "-c", "1024", "/repo/file.go"}); ok {
		t.Fatal("head -c (bytes mode) must not parse as line read")
	}
}

func TestCountReadPaths(t *testing.T) {
	t.Parallel()
	if n := countReadPaths([]string{"head", "-n", "5", "x.go"}); n != 1 {
		t.Fatalf("got %d", n)
	}
}

func TestCountReadPaths_flagAtEnd(t *testing.T) {
	t.Parallel()
	// Flag at very end with no following value — must not skip past end of slice
	if n := countReadPaths([]string{"head", "-n"}); n != 0 {
		t.Fatalf("flag at end: got %d", n)
	}
	if p := lastReadFilePath([]string{"head", "-n"}); p != "" {
		t.Fatalf("flag at end: got %q", p)
	}
}

// TestLastReadFilePath covers the flag-with-value and "-" stdin branches.
func TestLastReadFilePath(t *testing.T) {
	t.Parallel()
	// -n 5 file.go: flag with value, then file path.
	if p := lastReadFilePath([]string{"head", "-n", "5", "file.go"}); p != "file.go" {
		t.Fatalf("-n 5 file.go: got %q", p)
	}
	// "--lines" with value.
	if p := lastReadFilePath([]string{"head", "--lines", "10", "file.py"}); p != "file.py" {
		t.Fatalf("--lines 10 file.py: got %q", p)
	}
	// "-" as stdin placeholder: skipped, returns last real file.
	if p := lastReadFilePath([]string{"cat", "-", "real.go"}); p != "real.go" {
		t.Fatalf("cat - real.go: got %q", p)
	}
	// only "-" and flags: returns "".
	if p := lastReadFilePath([]string{"cat", "-", "-n", "5"}); p != "" {
		t.Fatalf("cat - -n 5: got %q", p)
	}
}

func TestReadPathFromCommandLine(t *testing.T) {
	t.Parallel()
	if got := ReadPathFromCommandLine("cat internal/filter/builtin_read.go"); got != "internal/filter/builtin_read.go" {
		t.Fatalf("read path = %q", got)
	}
	if got := ReadPathFromCommandLine("head -n 20 main.go"); got != "main.go" {
		t.Fatalf("head read path = %q", got)
	}
	if got := ReadPathFromCommandLine("sed -n '10,20p' main.go"); got != "main.go" {
		t.Fatalf("sed read path = %q", got)
	}
	if got := ReadPathFromCommandLine("awk 'NR>=10 && NR<=20 {print}' main.go"); got != "main.go" {
		t.Fatalf("awk read path = %q", got)
	}
	if got := ReadPathFromCommandLine("nl -ba main.go | sed -n '10,20p'"); got != "main.go" {
		t.Fatalf("nl|sed read path = %q", got)
	}
	if got := FullReadPathFromCommandLine("cat internal/filter/builtin_read.go"); got != "internal/filter/builtin_read.go" {
		t.Fatalf("full read path = %q", got)
	}
	if got := FullReadPathFromCommandLine("head -n 20 main.go"); got != "" {
		t.Fatalf("partial read must not be treated as full-file read, got %q", got)
	}
	for _, cmd := range []string{"cat a.go b.go", "cat main.go | wc -l", "go test ./...", "printf main.go"} {
		if got := ReadPathFromCommandLine(cmd); got != "" {
			t.Fatalf("command %q should not produce a single read path, got %q", cmd, got)
		}
	}
}

func TestReadRequestFromCommandLine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		command    string
		wantPath   string
		wantOffset int
		wantLimit  int
		wantOK     bool
	}{
		{name: "cat", command: "cat main.go", wantPath: "main.go", wantOK: true},
		{name: "head split", command: "head -n 20 main.go", wantPath: "main.go", wantOffset: 1, wantLimit: 20, wantOK: true},
		{name: "head short", command: "head -200 main.go", wantPath: "main.go", wantOffset: 1, wantLimit: 200, wantOK: true},
		{name: "tail split", command: "tail -n 20 main.go", wantPath: "main.go", wantOffset: -20, wantLimit: 20, wantOK: true},
		{name: "tail plus", command: "tail -n +42 main.go", wantPath: "main.go", wantOffset: 42, wantLimit: 0, wantOK: true},
		{name: "sed range", command: "sed -n '10,20p' main.go", wantPath: "main.go", wantOffset: 10, wantLimit: 11, wantOK: true},
		{name: "sed single", command: "sed -n 42p main.go", wantPath: "main.go", wantOffset: 42, wantLimit: 1, wantOK: true},
		{name: "nl sed range", command: "nl -ba main.go | sed -n '10,20p'", wantPath: "main.go", wantOffset: 10, wantLimit: 11, wantOK: true},
		{name: "nl sed single", command: "nl -ba main.go | sed -n 42p", wantPath: "main.go", wantOffset: 42, wantLimit: 1, wantOK: true},
		{name: "nl sed with format options", command: "nl -ba -w1 -s': ' main.go | sed -n '10,20p'", wantPath: "main.go", wantOffset: 10, wantLimit: 11, wantOK: true},
		{name: "awk range", command: "awk 'NR>=10 && NR<=20 {print}' main.go", wantPath: "main.go", wantOffset: 10, wantLimit: 11, wantOK: true},
		{name: "awk print zero", command: "awk 'NR>=10&&NR<=20{print $0}' main.go", wantPath: "main.go", wantOffset: 10, wantLimit: 11, wantOK: true},
		{name: "awk single", command: "awk 'NR==42{print}' main.go", wantPath: "main.go", wantOffset: 42, wantLimit: 1, wantOK: true},
		{name: "awk from", command: "awk 'NR>=42{print}' main.go", wantPath: "main.go", wantOffset: 42, wantLimit: 0, wantOK: true},
		{name: "awk until", command: "awk 'NR<=42{print}' main.go", wantPath: "main.go", wantOffset: 1, wantLimit: 42, wantOK: true},
		{name: "awk projection unsupported", command: "awk '{print $1}' main.go", wantOK: false},
		{name: "awk multi file unsupported", command: "awk 'NR>=10{print}' a.go b.go", wantOK: false},
		{name: "awk vars unsupported", command: "awk -v start=10 'NR>=start{print}' main.go", wantOK: false},
		{name: "nl default body unsupported", command: "nl main.go | sed -n '10,20p'", wantOK: false},
		{name: "nl sed multi file unsupported", command: "nl -ba a.go b.go | sed -n '10,20p'", wantOK: false},
		{name: "nl sed shellism unsupported", command: "nl -ba $FILE | sed -n '10,20p'", wantOK: false},
		{name: "nl sed dollar unsupported", command: "nl -ba main.go | sed -n '10,$p'", wantOK: false},
		{name: "byte head unsupported", command: "head -c 20 main.go", wantOK: false},
		{name: "compound unsupported", command: "head -n 20 main.go | cat", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := ReadRequestFromCommandLine(tt.command)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v (got %+v)", ok, tt.wantOK, got)
			}
			if !ok {
				return
			}
			if got.Path != tt.wantPath || got.Offset != tt.wantOffset || got.Limit != tt.wantLimit {
				t.Fatalf("request = %+v, want path=%q offset=%d limit=%d", got, tt.wantPath, tt.wantOffset, tt.wantLimit)
			}
		})
	}
}

func TestNormalizeReadCommandLine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		command string
		workdir string
		want    string
	}{
		{
			name:    "simple read",
			command: "sed -n '10,20p' docs/todo.md",
			workdir: "/repo/project",
			want:    "sed -n 10,20p /repo/project/docs/todo.md",
		},
		{
			name:    "nl sed pipeline",
			command: "nl -ba docs/todo.md | sed -n '10,20p'",
			workdir: "/repo/project",
			want:    "nl -ba /repo/project/docs/todo.md | sed -n 10,20p",
		},
		{
			name:    "absolute path unchanged",
			command: "nl -ba /repo/project/docs/todo.md | sed -n '10,20p'",
			workdir: "/repo/project",
			want:    "",
		},
		{
			name:    "non read unchanged",
			command: "go test ./...",
			workdir: "/repo/project",
			want:    "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := NormalizeReadCommandLine(tt.command, tt.workdir); got != tt.want {
				t.Fatalf("NormalizeReadCommandLine() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTailLineRange(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		argv       []string
		wantOffset int
		wantLimit  int
		wantOK     bool
	}{
		{"default_no_args", []string{"tail"}, -10, 10, true},
		{"n_separate", []string{"tail", "-n", "20"}, -20, 20, true},
		{"lines_separate", []string{"tail", "--lines", "5"}, -5, 5, true},
		{"n_attached", []string{"tail", "-n20"}, -20, 20, true},
		{"lines_eq", []string{"tail", "--lines=15"}, -15, 15, true},
		{"bare_number", []string{"tail", "-30"}, -30, 30, true},
		{"n_separate_missing_value", []string{"tail", "-n"}, 0, 0, false},
		{"lines_separate_missing_value", []string{"tail", "--lines"}, 0, 0, false},
		{"bytes_short_disables_range", []string{"tail", "-c"}, 0, 0, false},
		{"bytes_long_disables_range", []string{"tail", "--bytes"}, 0, 0, false},
		{"bytes_attached_disables_range", []string{"tail", "-c100"}, 0, 0, false},
		{"bytes_eq_disables_range", []string{"tail", "--bytes=100"}, 0, 0, false},
		{"n_separate_non_numeric", []string{"tail", "-n", "abc"}, 0, 0, false},
		{"n_attached_non_numeric", []string{"tail", "-nabc"}, 0, 0, false},
		{"bare_non_numeric_falls_through_to_default", []string{"tail", "-abc"}, -10, 10, true},
		{"plus_prefix", []string{"tail", "-n", "+5"}, 5, 0, true},
		{"flag_then_n", []string{"tail", "-q", "-n", "7"}, -7, 7, true},
		{"flag_then_bare", []string{"tail", "-q", "-42"}, -42, 42, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			off, lim, ok := tailLineRange(tc.argv)
			if ok != tc.wantOK || off != tc.wantOffset || lim != tc.wantLimit {
				t.Fatalf("tailLineRange(%v) = (%d, %d, %v), want (%d, %d, %v)",
					tc.argv, off, lim, ok, tc.wantOffset, tc.wantLimit, tc.wantOK)
			}
		})
	}
}

func TestIsFullFileCat(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		argv []string
		want bool
	}{
		{"empty", []string{}, false},
		{"cat_lower", []string{"cat"}, true},
		{"cat_upper", []string{"CAT"}, true},
		{"cat_path", []string{"/usr/bin/cat"}, true},
		{"cat_exe", []string{"cat.exe"}, false},
		{"head", []string{"head"}, false},
		{"tail", []string{"tail"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isFullFileCat(tc.argv); got != tc.want {
				t.Fatalf("isFullFileCat(%v) = %v, want %v", tc.argv, got, tc.want)
			}
		})
	}
}

func TestSedLineRange(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		argv       []string
		wantOffset int
		wantLimit  int
		wantOK     bool
	}{
		{"too_short", []string{"sed"}, 0, 0, false},
		{"missing_n_flag", []string{"sed", "1,2p"}, 0, 0, false},
		{"valid_range", []string{"sed", "-n", "10,20p"}, 10, 11, true},
		{"single_line", []string{"sed", "-n", "5p"}, 5, 1, true},
		{"no_p_suffix", []string{"sed", "-n", "10,20"}, 0, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			off, lim, ok := sedLineRange(tc.argv)
			if ok != tc.wantOK || off != tc.wantOffset || lim != tc.wantLimit {
				t.Fatalf("sedLineRange(%v) = (%d, %d, %v), want (%d, %d, %v)",
					tc.argv, off, lim, ok, tc.wantOffset, tc.wantLimit, tc.wantOK)
			}
		})
	}
}

func TestNLOptionConsumesNext(t *testing.T) {
	t.Parallel()
	consumes := []string{"-d", "-f", "-h", "-i", "-l", "-n", "-s", "-v", "-w",
		"--section-delimiter", "--footer-numbering", "--header-numbering",
		"--line-increment", "--join-blank-lines", "--number-format",
		"--number-separator", "--starting-line-number", "--number-width"}
	for _, arg := range consumes {
		if !nlOptionConsumesNext(arg) {
			t.Fatalf("nlOptionConsumesNext(%q) = false, want true", arg)
		}
	}
	nonConsumes := []string{"-ba", "-b", "-p", "--no-renumber", "--body-numbering", "file.txt", ""}
	for _, arg := range nonConsumes {
		if nlOptionConsumesNext(arg) {
			t.Fatalf("nlOptionConsumesNext(%q) = true, want false", arg)
		}
	}
}

func TestQuoteReadArg(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		arg  string
		want string
	}{
		{"empty", "", `""`},
		{"dollar_no_single_quote", "$HOME", "'$HOME'"},
		{"dollar_with_single_quote", "$HOME and '", ""},
		{"plain", "file.go", "file.go"},
		{"with_space", "file with space.go", strconv.Quote("file with space.go")},
		{"with_double_quote", `file"go`, strconv.Quote(`file"go`)},
		{"with_backslash", `file\go`, strconv.Quote(`file\go`)},
		{"with_pipe", "file|go", strconv.Quote("file|go")},
		{"with_amp", "file&go", strconv.Quote("file&go")},
		{"with_semicolon", "file;go", strconv.Quote("file;go")},
		{"with_lt_gt", "file<go>txt", strconv.Quote("file<go>txt")},
		{"with_star", "file*.go", strconv.Quote("file*.go")},
		{"with_question", "file?.go", strconv.Quote("file?.go")},
		{"with_parens", "file(go)", strconv.Quote("file(go)")},
		{"with_backtick", "file`go", strconv.Quote("file`go")},
		{"with_single_quote", "file'go", strconv.Quote("file'go")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := quoteReadArg(tc.arg)
			// Special case: dollar+single-quote path returns a quoted
			// form that strconv.Quote would also produce, but the
			// function uses single-quote wrapping instead. Check the
			// non-empty expectation for that case directly.
			if tc.name == "dollar_with_single_quote" {
				// strconv.Quote is the fallback path for $ + '.
				if got != strconv.Quote(tc.arg) {
					t.Fatalf("quoteReadArg(%q) = %q, want %q", tc.arg, got, strconv.Quote(tc.arg))
				}
				return
			}
			if got != tc.want {
				t.Fatalf("quoteReadArg(%q) = %q, want %q", tc.arg, got, tc.want)
			}
		})
	}
}
