package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os/exec"
	"sync"
)

// Executor runs commands for testable tool implementations.
type Executor interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// OSExecutor uses the real os/exec package.
type OSExecutor struct{}

// Runs a command with context, process-group isolation, and
// context-aware pipe reads. Returns combined output or error.
func (OSExecutor) Run(
	ctx context.Context, name string, args ...string,
) ([]byte, error) {
	cmd := exec.Command(name, args...)
	setupProcessGroup(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start: %w", err)
	}

	go func() {
		<-ctx.Done()
		if cmd.Process != nil {
			killProcessGroup(cmd.Process.Pid)
		}
	}()

	var outBuf, errBuf bytes.Buffer
	// readDone carries pipe-read errors so callers can detect data
	// loss from closed pipes or I/O failures instead of silently
	// ignoring them (issue #49).
	readDone := make(chan error, 2)

	go func() {
		_, err := io.Copy(&outBuf, &contextReader{r: stdout, ctx: ctx})
		readDone <- err
	}()
	go func() {
		_, err := io.Copy(&errBuf, &contextReader{r: stderr, ctx: ctx})
		readDone <- err
	}()

	for i := 0; i < 2; i++ {
		if rErr := <-readDone; rErr != nil && !errors.Is(rErr, context.Canceled) {
			// Log pipe read errors that aren't simply context
			// cancellation — these indicate real data loss.
			slog.Warn("exec pipe read error", "err", rErr)
		}
	}

	waitErr := cmd.Wait()

	combined := append(outBuf.Bytes(), errBuf.Bytes()...)

	if ctx.Err() != nil {
		return combined, fmt.Errorf("exec: %w", ctx.Err())
	}
	if waitErr != nil {
		return combined, waitErr
	}

	return combined, nil
}

// HTTPClient performs HTTP requests. Used for testable web tools.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Wraps an io.Reader and returns context.Canceled when the
// context is done, even if the underlying Read() blocks.
// Uses a single background goroutine to avoid spawning
// one goroutine per Read() call.
type contextReader struct {
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

// Starts the single background reader goroutine.
func (cr *contextReader) startReader() {
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

func (cr *contextReader) Read(p []byte) (int, error) {
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
