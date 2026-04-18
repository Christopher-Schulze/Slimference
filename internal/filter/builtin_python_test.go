package filter

import (
	"strings"
	"testing"
)

// TestTryCompactPythonTraceback_passthroughWithoutAnchor leaves non-traceback
// output unchanged.
func TestTryCompactPythonTraceback_passthroughWithoutAnchor(t *testing.T) {
	t.Parallel()
	in := []byte("hello world\nnothing to see here\n")
	out, ok := TryCompactPythonTraceback(in)
	if ok {
		t.Fatalf("expected passthrough, got transformed: %s", out)
	}
	if string(out) != string(in) {
		t.Fatal("output must be identical to input when not a traceback")
	}
}

// TestTryCompactPythonTraceback_shortTracebackIsPassthrough keeps small
// tracebacks unchanged (zero-downside: only emit when strictly shorter).
func TestTryCompactPythonTraceback_shortTracebackIsPassthrough(t *testing.T) {
	t.Parallel()
	in := []byte(`Traceback (most recent call last):
  File "a.py", line 1, in <module>
ValueError: nope
`)
	out, ok := TryCompactPythonTraceback(in)
	if ok {
		t.Fatalf("short traceback must pass through, got: %s", out)
	}
	_ = out
}

// TestTryCompactPythonTraceback_elidesLibraryFrames keeps user frames and
// the final frame, collapses library frames into a single note.
func TestTryCompactPythonTraceback_elidesLibraryFrames(t *testing.T) {
	t.Parallel()
	in := []byte(`Traceback (most recent call last):
  File "/Users/me/app/main.py", line 42, in <module>
    run()
  File "/Users/me/app/core.py", line 10, in run
    do_thing()
  File "/Users/me/.venv/lib/python3.11/site-packages/requests/adapters.py", line 519, in send
    resp = conn.urlopen(...)
  File "/Users/me/.venv/lib/python3.11/site-packages/urllib3/connectionpool.py", line 789, in urlopen
    response = self._make_request(...)
  File "/Users/me/.venv/lib/python3.11/site-packages/urllib3/connection.py", line 451, in _make_request
    self._validate_conn(conn)
  File "/Users/me/app/fail.py", line 5, in _validate_conn
    raise ConnectionError("boom")
ConnectionError: boom
`)
	out, ok := TryCompactPythonTraceback(in)
	if !ok {
		t.Fatal("expected compression to fire")
	}
	s := string(out)
	if !strings.Contains(s, "ConnectionError: boom") {
		t.Fatalf("terminal exception missing: %s", s)
	}
	if !strings.Contains(s, "/Users/me/app/main.py") {
		t.Fatalf("first user frame missing: %s", s)
	}
	if !strings.Contains(s, "fail.py") {
		t.Fatalf("final frame missing: %s", s)
	}
	if !strings.Contains(s, "library frames elided") {
		t.Fatalf("missing elision note: %s", s)
	}
	if strings.Contains(s, "urllib3/connectionpool.py") {
		t.Fatalf("library frame leaked: %s", s)
	}
	if len(out) >= len(in) {
		t.Fatalf("output must be strictly shorter: in=%d out=%d", len(in), len(out))
	}
}

// TestTryCompactPythonTraceback_chainedException preserves each chain
// boundary and re-enters compression for every inner block.
func TestTryCompactPythonTraceback_chainedException(t *testing.T) {
	t.Parallel()
	in := []byte(`Traceback (most recent call last):
  File "/Users/me/app/parse.py", line 15, in parse
    int(x)
  File "/Users/me/.venv/lib/python3.11/site-packages/noise/inner.py", line 3, in inner
    raise ValueError("bad")
  File "/Users/me/app/more.py", line 8, in more
    raise ValueError("bad")
ValueError: invalid literal for int() with base 10: 'x'

During handling of the above exception, another exception occurred:

Traceback (most recent call last):
  File "/Users/me/app/main.py", line 20, in <module>
    parse('x')
  File "/Users/me/.venv/lib/python3.11/site-packages/utils/helper.py", line 99, in run
    result = parse(val)
  File "/Users/me/app/adapter.py", line 5, in adapter
    result = parse(val)
RuntimeError: parsing failed
`)
	out, ok := TryCompactPythonTraceback(in)
	if !ok {
		t.Fatal("expected compression")
	}
	s := string(out)
	if !strings.Contains(s, "During handling of the above exception, another exception occurred:") {
		t.Fatalf("chain marker lost: %s", s)
	}
	if !strings.Contains(s, "ValueError: invalid literal") {
		t.Fatalf("first exception lost: %s", s)
	}
	if !strings.Contains(s, "RuntimeError: parsing failed") {
		t.Fatalf("second exception lost: %s", s)
	}
	if strings.Contains(s, "noise/inner.py") {
		t.Fatalf("library frame leaked: %s", s)
	}
	if strings.Contains(s, "utils/helper.py") {
		t.Fatalf("library frame leaked: %s", s)
	}
}

// TestBytesHasPythonTraceback covers the helper predicate.
func TestBytesHasPythonTraceback(t *testing.T) {
	t.Parallel()
	if !bytesHasPythonTraceback([]byte("Traceback (most recent call last):\n...")) {
		t.Fatal("expected anchor detection")
	}
	if bytesHasPythonTraceback([]byte("nothing here")) {
		t.Fatal("unexpected anchor detection")
	}
}

// TestTryCompactPythonTraceback_unknownMiddle triggers findTracebackEnd's
// "stop conservatively" branch when the block has unknown continuation.
func TestTryCompactPythonTraceback_unknownMiddle(t *testing.T) {
	t.Parallel()
	in := []byte(`Traceback (most recent call last):
  File "/Users/me/app/main.py", line 1, in <module>
some stray line that is not a frame, body, chain marker, nor exception
more content after
`)
	// The function must not panic and should pass through because no
	// shorter output can be produced.
	out, ok := TryCompactPythonTraceback(in)
	_ = ok
	if string(out) == "" {
		t.Fatal("empty output is not acceptable")
	}
}

// TestLooksLikeExceptionLine exercises the exception-line predicate
// directly so every branch is covered deterministically.
func TestLooksLikeExceptionLine(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want bool
	}{
		{"ValueError: bad", true},
		{"My.Custom.Error: bad", true},
		{"", false},
		{"no colon here", false},
		{":leading colon", false},
		{"lower_case: bad", false},          // lowercase first letter
		{"Bad name!: bad", false},            // invalid character
		{"V: bad", true},                     // single uppercase letter is valid
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			if got := looksLikeExceptionLine(tc.in); got != tc.want {
				t.Errorf("looksLikeExceptionLine(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestIndentedNote covers singular and plural formatting.
func TestIndentedNote(t *testing.T) {
	t.Parallel()
	if got := indentedNote(1); got != "  [... 1 library frame elided]" {
		t.Fatalf("singular: %q", got)
	}
	if got := indentedNote(5); got != "  [... 5 library frames elided]" {
		t.Fatalf("plural: %q", got)
	}
}

// TestItoa covers the local int-to-string helper on zero, positive and
// negative inputs.
func TestItoa(t *testing.T) {
	t.Parallel()
	if itoa(0) != "0" {
		t.Fatal("zero")
	}
	if itoa(42) != "42" {
		t.Fatal("42")
	}
	if itoa(-17) != "-17" {
		t.Fatal("-17")
	}
}

// TestTryCompactPythonTraceback_endsWithBlankLine covers the
// findTracebackEnd branch where a blank line after the exception ends the
// block.
func TestTryCompactPythonTraceback_endsWithBlankLine(t *testing.T) {
	t.Parallel()
	in := []byte(`Traceback (most recent call last):
  File "/Users/me/app/a.py", line 1, in <module>
    call()
  File "/Users/me/.venv/lib/python3.11/site-packages/x/y.py", line 2, in call
    raise ValueError("x")
  File "/Users/me/app/b.py", line 3, in last
    final()
ValueError: x

trailing unrelated text line
another trailing line`)
	out, ok := TryCompactPythonTraceback(in)
	if !ok {
		t.Fatal("expected compression")
	}
	s := string(out)
	if !strings.Contains(s, "trailing unrelated text line") {
		t.Fatalf("trailing content lost: %s", s)
	}
	if !strings.Contains(s, "ValueError: x") {
		t.Fatalf("exception missing: %s", s)
	}
}

// TestTryCompactPythonTraceback_sourceLineInsideFrame covers the branch
// where a frame body (source line indented with four spaces) is kept.
func TestTryCompactPythonTraceback_sourceLineInsideFrame(t *testing.T) {
	t.Parallel()
	in := []byte(`Traceback (most recent call last):
  File "/Users/me/app/a.py", line 1, in <module>
    call_me()
  File "/Users/me/.venv/lib/python3.11/site-packages/x/y.py", line 2, in helper
    raise ValueError("x")
  File "/Users/me/app/b.py", line 3, in last
    final()
ValueError: x
`)
	out, ok := TryCompactPythonTraceback(in)
	if !ok {
		t.Fatal("expected compression")
	}
	s := string(out)
	if !strings.Contains(s, "call_me()") {
		t.Fatalf("source line for first frame lost: %s", s)
	}
	if !strings.Contains(s, "final()") {
		t.Fatalf("source line for final frame lost: %s", s)
	}
}

// TestApplyLayer0Filters_pythonTracebackDispatch proves the dispatcher routes
// traceback input through TryCompactPythonTraceback.
func TestApplyLayer0Filters_pythonTracebackDispatch(t *testing.T) {
	t.Parallel()
	in := []byte(`Traceback (most recent call last):
  File "/Users/me/app/a.py", line 10, in <module>
  File "/Users/me/.venv/lib/python3.11/site-packages/x/y.py", line 1, in helper
  File "/Users/me/.venv/lib/python3.11/site-packages/x/z.py", line 2, in h2
  File "/Users/me/.venv/lib/python3.11/site-packages/x/w.py", line 3, in h3
  File "/Users/me/app/b.py", line 20, in tail
ValueError: boom
`)
	out, name := applyLayer0Filters("", []string{"python", "run.py"}, in)
	if name != "python_traceback" {
		t.Fatalf("expected python_traceback dispatch, got %q", name)
	}
	if len(out) >= len(in) {
		t.Fatalf("dispatch must produce a strictly shorter output: in=%d out=%d", len(in), len(out))
	}
}

// TestTryCompactPythonTraceback_runsOffEndOfInput covers findTracebackEnd's
// return len(lines) branch when the traceback is the last thing in stdout
// and ends without trailing content.
func TestTryCompactPythonTraceback_runsOffEndOfInput(t *testing.T) {
	t.Parallel()
	in := []byte(`Traceback (most recent call last):
  File "/Users/me/app/a.py", line 1, in <module>
  File "/Users/me/.venv/lib/python3.11/site-packages/x/y.py", line 2, in helper
  File "/Users/me/.venv/lib/python3.11/site-packages/x/z.py", line 3, in h2
  File "/Users/me/.venv/lib/python3.11/site-packages/x/w.py", line 4, in h3
  File "/Users/me/app/b.py", line 5, in last
ValueError: boom`)
	out, ok := TryCompactPythonTraceback(in)
	if !ok {
		t.Fatal("expected compression")
	}
	if !strings.Contains(string(out), "ValueError: boom") {
		t.Fatalf("exception missing: %s", out)
	}
}

// TestTryCompactPythonTraceback_blankLineBeforeExceptionContinues covers the
// "line == \"\" && !sawException -> continue" branch of findTracebackEnd.
func TestTryCompactPythonTraceback_blankLineBeforeExceptionContinues(t *testing.T) {
	t.Parallel()
	// The blank line between the final frame's source body and the exception
	// line exercises the pre-exception blank handling.
	in := []byte(`Traceback (most recent call last):
  File "/Users/me/app/a.py", line 1, in <module>
    call()
  File "/Users/me/.venv/lib/python3.11/site-packages/x/y.py", line 2, in call
    raise
  File "/Users/me/.venv/lib/python3.11/site-packages/x/z.py", line 3, in h2
    raise
  File "/Users/me/.venv/lib/python3.11/site-packages/x/w.py", line 4, in h3
    raise
  File "/Users/me/app/b.py", line 5, in last
    final()

ValueError: boom
`)
	out, ok := TryCompactPythonTraceback(in)
	if !ok {
		t.Fatal("expected compression")
	}
	s := string(out)
	if !strings.Contains(s, "ValueError: boom") {
		t.Fatalf("exception lost: %s", s)
	}
	if !strings.Contains(s, "final()") {
		t.Fatalf("final frame body lost: %s", s)
	}
}

// TestIsLibraryFrameHeader covers both library and user-code frames.
func TestIsLibraryFrameHeader(t *testing.T) {
	t.Parallel()
	if !isLibraryFrameHeader(`  File "/Users/me/.venv/lib/python3.11/site-packages/requests/adapters.py", line 519, in send`) {
		t.Fatal("site-packages not detected")
	}
	if isLibraryFrameHeader(`  File "/Users/me/app/main.py", line 42, in <module>`) {
		t.Fatal("user code misclassified as library")
	}
	if isLibraryFrameHeader("not a frame at all") {
		t.Fatal("non-frame must not match")
	}
}
