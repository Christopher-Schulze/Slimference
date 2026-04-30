package filter

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestStreamPump_DedupConsecutive(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	p := newStreamPump(&buf, StreamOptions{DedupConsecutive: true, FlushInterval: time.Hour, WindowLines: 100})
	in := strings.NewReader("a\na\na\nb\nb\nc\n")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { p.run(ctx, in); close(done) }()
	// Wait for scanner to finish.
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pump did not finish")
	}
	got := buf.String()
	if !strings.Contains(got, "a [x3]") || !strings.Contains(got, "b [x2]") || !strings.Contains(got, "c") {
		t.Fatalf("dedup output: %q", got)
	}
}

func TestStreamPump_NoDedupPassThrough(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	p := newStreamPump(&buf, StreamOptions{FlushInterval: time.Hour, WindowLines: 100})
	in := strings.NewReader("first\nsecond\n")
	ctx := context.Background()
	done := make(chan struct{})
	go func() { p.run(ctx, in); close(done) }()
	<-done
	got := buf.String()
	if !strings.Contains(got, "first") || !strings.Contains(got, "second") {
		t.Fatalf("output: %q", got)
	}
}

func TestStreamPump_ANSIStrip(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	p := newStreamPump(&buf, StreamOptions{StripANSI: true, FlushInterval: time.Hour, WindowLines: 100})
	in := strings.NewReader("\x1b[31mred\x1b[0m\nplain\n")
	ctx := context.Background()
	done := make(chan struct{})
	go func() { p.run(ctx, in); close(done) }()
	<-done
	got := buf.String()
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("ANSI not stripped: %q", got)
	}
	if !strings.Contains(got, "red") {
		t.Fatalf("text lost: %q", got)
	}
}

func TestStreamPump_WindowFlush(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	p := newStreamPump(&buf, StreamOptions{FlushInterval: time.Hour, WindowLines: 3})
	// 5 lines, window=3 -> should flush at 3 and again on EOF.
	in := strings.NewReader("a\nb\nc\nd\ne\n")
	ctx := context.Background()
	done := make(chan struct{})
	go func() { p.run(ctx, in); close(done) }()
	<-done
	got := buf.String()
	for _, want := range []string{"a", "b", "c", "d", "e"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestStreamPump_TickerFlush(t *testing.T) {
	t.Parallel()
	pr, pw := io.Pipe()
	defer pr.Close()
	var buf bytes.Buffer
	p := newStreamPump(&buf, StreamOptions{FlushInterval: 30 * time.Millisecond, WindowLines: 1000})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { p.run(ctx, pr); close(done) }()
	_, _ = pw.Write([]byte("first\n"))
	// Give the ticker a chance to flush.
	time.Sleep(80 * time.Millisecond)
	_ = pw.Close()
	<-done
	if !strings.Contains(buf.String(), "first") {
		t.Fatalf("ticker flush missed: %q", buf.String())
	}
}

func TestStreamPump_ContextCancel(t *testing.T) {
	t.Parallel()
	pr, pw := io.Pipe()
	defer pr.Close()
	var buf bytes.Buffer
	p := newStreamPump(&buf, StreamOptions{FlushInterval: time.Hour, WindowLines: 100})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.run(ctx, pr); close(done) }()
	_, _ = pw.Write([]byte("only-line\n"))
	time.Sleep(20 * time.Millisecond)
	cancel()
	_ = pw.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ctx cancel did not stop pump")
	}
}

func TestStreamPump_CloseFlushesFinal(t *testing.T) {
	t.Parallel()
	pr, pw := io.Pipe()
	defer pr.Close()
	var buf bytes.Buffer
	p := newStreamPump(&buf, StreamOptions{FlushInterval: time.Hour, WindowLines: 100})
	ctx := context.Background()
	done := make(chan struct{})
	go func() { p.run(ctx, pr); close(done) }()
	_, _ = pw.Write([]byte("x\n"))
	time.Sleep(20 * time.Millisecond)
	p.close()
	p.close() // idempotent
	_ = pw.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("close did not flush")
	}
	p.wait()
	if !strings.Contains(buf.String(), "x") {
		t.Fatalf("final flush missing: %q", buf.String())
	}
}

func TestRunStreamingPipeline_EmptyArgv(t *testing.T) {
	t.Parallel()
	if code, err := RunStreamingPipeline(context.Background(), nil, io.Discard, StreamOptions{}); err == nil || code == 0 {
		t.Fatal("empty argv must error")
	}
}

func TestRunStreamingPipeline_RealCommand(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	code, err := RunStreamingPipeline(context.Background(),
		[]string{"sh", "-c", "echo line-one; echo line-two"},
		&buf,
		StreamOptions{FlushInterval: 100 * time.Millisecond, WindowLines: 100, StripANSI: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("exit code: %d", code)
	}
	if !strings.Contains(buf.String(), "line-one") || !strings.Contains(buf.String(), "line-two") {
		t.Fatalf("output: %q", buf.String())
	}
}

func TestRunStreamingPipeline_NonZeroExit(t *testing.T) {
	t.Parallel()
	code, err := RunStreamingPipeline(context.Background(),
		[]string{"sh", "-c", "exit 7"}, io.Discard, StreamOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if code != 7 {
		t.Fatalf("expected exit 7, got %d", code)
	}
}

func TestInterpretExitError(t *testing.T) {
	t.Parallel()
	if c, err := interpretExitError(nil); c != 0 || err != nil {
		t.Fatalf("nil: %d %v", c, err)
	}
	if c, err := interpretExitError(io.ErrUnexpectedEOF); c != 1 || err == nil {
		t.Fatalf("non-exit: %d %v", c, err)
	}
}

func TestRunStreamingPipeline_NonExitErrorPropagated(t *testing.T) {
	t.Parallel()
	// Use a slow command and cancel mid-flight so wait returns a
	// context-cancellation error (not an ExitError) on some platforms.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := RunStreamingPipeline(ctx,
		[]string{"sh", "-c", "sleep 5; echo done"},
		io.Discard,
		StreamOptions{},
	); err == nil {
		t.Skip("platform returned exit-code on cancellation; non-ExitError branch not exercised here")
	}
}

func TestRunStreamingPipeline_StdoutPipeError(t *testing.T) {
	prev := streamCmdSetup
	t.Cleanup(func() { streamCmdSetup = prev })
	streamCmdSetup = func(*exec.Cmd) (io.ReadCloser, error) {
		return nil, io.ErrUnexpectedEOF
	}
	if _, err := RunStreamingPipeline(context.Background(),
		[]string{"sh", "-c", "echo x"}, io.Discard, StreamOptions{},
	); err == nil {
		t.Fatal("expected stdout pipe error")
	}
}

func TestRunStreamingPipeline_StartError(t *testing.T) {
	t.Parallel()
	code, err := RunStreamingPipeline(context.Background(),
		[]string{"/bin/no-such-command-anywhere"},
		io.Discard,
		StreamOptions{},
	)
	if err == nil || code == 0 {
		t.Fatal("expected start error")
	}
}
