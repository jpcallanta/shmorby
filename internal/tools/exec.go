package tools

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"syscall"
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
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

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
			syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
	}()

	var outBuf, errBuf bytes.Buffer
	readDone := make(chan struct{}, 2)

	go func() {
		io.Copy(&outBuf, &contextReader{r: stdout, ctx: ctx})
		readDone <- struct{}{}
	}()
	go func() {
		io.Copy(&errBuf, &contextReader{r: stderr, ctx: ctx})
		readDone <- struct{}{}
	}()

	for i := 0; i < 2; i++ {
		<-readDone
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

// Wraps an io.Reader and returns context.Canceled when the
// context is done, even if the underlying Read() blocks.
type contextReader struct {
	r   io.Reader
	ctx context.Context
}

func (cr *contextReader) Read(p []byte) (int, error) {
	type result struct {
		n   int
		err error
	}
	ch := make(chan result, 1)
	go func() {
		n, err := cr.r.Read(p)
		ch <- result{n, err}
	}()
	select {
	case r := <-ch:
		return r.n, r.err
	case <-cr.ctx.Done():
		return 0, cr.ctx.Err()
	}
}
