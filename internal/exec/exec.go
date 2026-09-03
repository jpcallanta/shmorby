// Package exec provides a unified Executor interface and OSExecutor
// implementation for running external commands with process-group
// isolation and context-aware pipe reads.
//
// This package consolidates the duplicate Executor interfaces that
// previously existed in internal/tools, internal/tui/navigation,
// and internal/health.
package exec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"sync"
	"time"

	"shmorby/internal/health"
)

// Executor runs external commands. All consumers (tools, TUI,
// health) should use this single interface.
type Executor interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// MaxOutputBufferBytes caps how many bytes per output stream are
// buffered while a command runs; a runaway command must not grow
// the buffer without bound. Shared with internal/tools so both
// exec paths agree on the cap. Var so tests can shrink it.
var MaxOutputBufferBytes int64 = 100 << 20 // 100 MiB

// OSExecutor uses the real os/exec package with process-group
// isolation and context-aware pipe reads. Failures are wrapped
// as health.Degraded for structured diagnostics.
type OSExecutor struct{}

// Run executes a command with context, process-group isolation,
// and context-aware pipe reads. Returns combined output or error.
func (OSExecutor) Run(
	ctx context.Context, name string, args ...string,
) ([]byte, error) {
	start := time.Now()

	cmd := exec.Command(name, args...)
	SetupProcessGroup(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, health.Wrap("exec", time.Since(start),
			fmt.Errorf("stdout pipe: %w", err))
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, health.Wrap("exec", time.Since(start),
			fmt.Errorf("stderr pipe: %w", err))
	}

	if err := cmd.Start(); err != nil {
		return nil, health.Wrap("exec", time.Since(start),
			fmt.Errorf("start: %w", err))
	}

	go func() {
		<-ctx.Done()
		if cmd.Process != nil {
			KillProcessGroup(cmd.Process.Pid)
		}
	}()

	var outBuf, errBuf bytes.Buffer
	// readDone carries pipe-read errors so callers can detect
	// data loss from closed pipes or I/O failures instead of
	// silently ignoring them.
	readDone := make(chan error, 2)

	// LimitReader caps buffered bytes per stream so a runaway
	// command cannot exhaust memory; truncation of the result to
	// the configured output limit stays the caller's job.
	go func() {
		_, err := io.Copy(&outBuf, NewContextReader(
			io.LimitReader(stdout, MaxOutputBufferBytes), ctx))
		readDone <- err
	}()
	go func() {
		_, err := io.Copy(&errBuf, NewContextReader(
			io.LimitReader(stderr, MaxOutputBufferBytes), ctx))
		readDone <- err
	}()

	for i := 0; i < 2; i++ {
		if rErr := <-readDone; rErr != nil &&
			!errors.Is(rErr, context.Canceled) {
			// Log pipe read errors that aren't simply
			// context cancellation — these indicate real
			// data loss.
			slog.Warn("exec pipe read error", "err", rErr)
		}
	}

	waitErr := cmd.Wait()

	combined := append(outBuf.Bytes(), errBuf.Bytes()...)

	// Flag the hard buffer cap so a consumer sees the truncation
	// even when no output limit is configured.
	if int64(outBuf.Len()) >= MaxOutputBufferBytes ||
		int64(errBuf.Len()) >= MaxOutputBufferBytes {
		combined = append(combined, fmt.Sprintf(
			"\n... (stream capped at %d bytes)",
			MaxOutputBufferBytes,
		)...)
	}

	elapsed := time.Since(start)

	if ctx.Err() != nil {
		return combined, health.Wrap(
			"exec", elapsed,
			fmt.Errorf("exec: %w", ctx.Err()),
		)
	}
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			// Non-zero exit is not degraded; surface as
			// stdout.
			return combined, waitErr
		}

		return combined, health.Wrap("exec", elapsed, waitErr)
	}

	return combined, nil
}

// ContextReader wraps an io.Reader and returns context.Canceled
// when the context is done, even if the underlying Read() blocks.
// Uses a single background goroutine to avoid spawning one
// goroutine per Read() call.
type ContextReader struct {
	r   io.Reader
	ctx context.Context

	once   sync.Once
	ch     chan readResult
	remain []byte
}

type readResult struct {
	data []byte
	err  error
}

// NewContextReader creates a context-aware reader that returns
// ctx.Err() when the context is cancelled, even if the
// underlying Read() blocks.
func NewContextReader(
	r io.Reader, ctx context.Context,
) *ContextReader {
	return &ContextReader{r: r, ctx: ctx}
}

// startReader starts the single background reader goroutine.
func (cr *ContextReader) startReader() {
	cr.ch = make(chan readResult, 1)
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := cr.r.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				select {
				case cr.ch <- readResult{data: chunk}:
				case <-cr.ctx.Done():
					return
				}
			}
			if err != nil {
				cr.ch <- readResult{err: err}
				return
			}
		}
	}()
}

func (cr *ContextReader) Read(p []byte) (int, error) {
	if len(cr.remain) > 0 {
		n := copy(p, cr.remain)
		cr.remain = cr.remain[n:]

		return n, nil
	}

	cr.once.Do(cr.startReader)

	select {
	case <-cr.ctx.Done():
		return 0, cr.ctx.Err()
	case rr := <-cr.ch:
		if rr.err != nil {
			return 0, rr.err
		}
		n := copy(p, rr.data)
		if n < len(rr.data) {
			cr.remain = rr.data[n:]
		}

		return n, nil
	}
}
