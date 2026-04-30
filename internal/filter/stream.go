package filter

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"

	"github.com/slimference/slimference/internal/compression"
)

// StreamOptions configures the streaming-aware Layer 0 filter. T94.
type StreamOptions struct {
	WindowLines      int           // sliding window for dedup; default 200
	FlushInterval    time.Duration // periodic flush; default 2s
	StripANSI        bool          // strip ANSI escape codes
	DedupConsecutive bool          // collapse consecutive identical lines
}

// streamDefaults applies sensible fallbacks when caller leaves zero
// values.
func (o *StreamOptions) streamDefaults() {
	if o.WindowLines <= 0 {
		o.WindowLines = 200
	}
	if o.FlushInterval <= 0 {
		o.FlushInterval = 2 * time.Second
	}
}

// streamCmdSetup is overridable in tests so the StdoutPipe error path
// can be exercised without producing a real os.Pipe failure.
var streamCmdSetup = func(cmd *exec.Cmd) (io.ReadCloser, error) {
	return cmd.StdoutPipe()
}

// RunStreamingPipeline runs argv as a subprocess and emits a compacted
// stream to out. T94. The pump:
//
//   - reads stdout line-by-line in a goroutine,
//   - applies optional ANSI-strip per line,
//   - collapses consecutive identical lines into "<line> [xN]",
//   - flushes every FlushInterval or when the rolling window fills,
//   - flushes a final batch on EOF / context cancellation,
//   - returns (exit_code, error). Empty argv is an error.
func RunStreamingPipeline(ctx context.Context, argv []string, out io.Writer, opts StreamOptions) (int, error) {
	if len(argv) == 0 {
		return 1, fmt.Errorf("empty argv")
	}
	opts.streamDefaults()

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	stdout, err := streamCmdSetup(cmd)
	if err != nil {
		return 1, fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return 1, fmt.Errorf("start: %w", err)
	}

	pump := newStreamPump(out, opts)
	go pump.run(ctx, stdout)

	// pump.wait() must finish BEFORE cmd.Wait(): cmd.Wait closes the
	// stdout pipe right after the subprocess exits, and any reader
	// still scanning the pipe at that point gets a short-read /
	// EBADF and drops buffered output. By waiting for the pump to
	// drain to EOF first, we guarantee scanner has consumed every
	// byte before the OS rugs the pipe out from under it.
	pump.wait()
	waitErr := cmd.Wait()

	return interpretExitError(waitErr)
}

// interpretExitError translates the cmd.Wait error into the (exit_code,
// err) tuple used by RunStreamingPipeline. Extracted for testability so
// the non-ExitError branch can be exercised without spawning a real
// process. T94.
func interpretExitError(waitErr error) (int, error) {
	if waitErr == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	return 1, waitErr
}

// streamPump consumes a Reader line-by-line and emits a compacted view
// to its writer. Internal helper for RunStreamingPipeline; exposed in
// tests via the synthetic-reader path.
type streamPump struct {
	out     io.Writer
	opts    StreamOptions
	mu      sync.Mutex
	last    string
	repeat  int
	pending []string
	done    chan struct{}
	started chan struct{}
	wg      sync.WaitGroup
}

func newStreamPump(out io.Writer, opts StreamOptions) *streamPump {
	opts.streamDefaults()
	return &streamPump{
		out:     out,
		opts:    opts,
		done:    make(chan struct{}),
		started: make(chan struct{}),
	}
}

// run reads from r line-by-line and feeds the dedup/window logic.
// Periodic flushes happen on FlushInterval; final flush on EOF.
//
// wg.Add(1) runs synchronously at the top of run() and `started` is
// closed immediately after, so that a caller invoking p.wait() before
// the goroutine has actually executed still blocks correctly. Without
// the started gate, wait() would hit wg.Wait() with counter=0 and
// return immediately, dropping output from a fast-exiting subprocess.
func (p *streamPump) run(ctx context.Context, r io.Reader) {
	p.wg.Add(1)
	close(p.started)
	defer p.wg.Done()

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	ticker := time.NewTicker(p.opts.FlushInterval)
	defer ticker.Stop()

	lines := make(chan string, 256)
	go func() {
		defer close(lines)
		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			case lines <- scanner.Text():
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			p.flush(true)
			return
		case <-p.done:
			p.flush(true)
			return
		case <-ticker.C:
			p.flush(false)
		case line, ok := <-lines:
			if !ok {
				p.flush(true)
				return
			}
			p.observe(line)
		}
	}
}

func (p *streamPump) observe(raw string) {
	line := raw
	if p.opts.StripANSI {
		line = compression.StripANSICodes(line)
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.opts.DedupConsecutive && line == p.last && p.last != "" {
		p.repeat++
		return
	}
	p.flushRepeatLocked()
	p.last = line
	p.repeat = 0
	p.pending = append(p.pending, line)
	if len(p.pending) >= p.opts.WindowLines {
		p.flushPendingLocked()
	}
}

func (p *streamPump) flushRepeatLocked() {
	if p.repeat == 0 || p.last == "" {
		return
	}
	p.pending = append(p.pending, fmt.Sprintf("%s [x%d]", p.last, p.repeat+1))
	p.repeat = 0
}

func (p *streamPump) flushPendingLocked() {
	for _, ln := range p.pending {
		_, _ = fmt.Fprintln(p.out, ln)
	}
	p.pending = p.pending[:0]
}

// flush emits any buffered lines. final=true means flush + reset
// dedup state (called at EOF).
func (p *streamPump) flush(final bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.flushRepeatLocked()
	p.flushPendingLocked()
	if final {
		p.last = ""
		p.repeat = 0
	}
}

// close signals the pump to stop after the current window.
func (p *streamPump) close() {
	select {
	case <-p.done:
		return
	default:
		close(p.done)
	}
}

// wait blocks until the pump goroutine has finished its final flush.
func (p *streamPump) wait() {
	// Wait for run() to have entered + executed wg.Add(1) before
	// blocking on Wait. Otherwise a fast subprocess can finish before
	// the runtime schedules the run goroutine, leaving wg counter at
	// zero and dropping any buffered output.
	<-p.started
	p.wg.Wait()
}
