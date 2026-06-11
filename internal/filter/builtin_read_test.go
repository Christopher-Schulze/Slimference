package filter

import "testing"

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
		tt := tt
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
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := NormalizeReadCommandLine(tt.command, tt.workdir); got != tt.want {
				t.Fatalf("NormalizeReadCommandLine() = %q, want %q", got, tt.want)
			}
		})
	}
}
