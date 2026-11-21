package execution

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestManager_Begin_PersistsPendingApproval verifies a row exists
// before permission evaluation.
func TestManager_Begin_PersistsPendingApproval(t *testing.T) {
	rec := newMemRecorder()
	m := NewManager(rec)

	h, err := m.Begin(context.Background(), New{
		SessionID:  "s1",
		ToolCallID: "call_1",
		Tool:       "shell",
		Command:    "df -h",
		ArgsHash:   "abc",
	})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}

	got := rec.get(h.ID())
	if got.Status != StatusPendingApproval {
		t.Errorf("status = %q, want %q",
			got.Status, StatusPendingApproval)
	}
	if got.ToolCallID != "call_1" || got.SessionID != "s1" {
		t.Errorf("record mismatch: %+v", got)
	}
}

// TestManager_Cancel_Confirmed verifies cancellation waits for the
// handle to finish before reporting success.
func TestManager_Cancel_Confirmed(t *testing.T) {
	rec := newMemRecorder()
	m := NewManager(rec)

	h, err := m.Begin(context.Background(), New{
		Tool: "shell", Command: "sleep 30",
	})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}

	execCtx := h.Start(context.Background())
	go func() {
		<-execCtx.Done()
		h.Finish("", execCtx.Err())
	}()

	if err := m.Cancel(h.ID()); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	got := rec.get(h.ID())
	if got.Status != StatusCancelled {
		t.Errorf("status = %q, want %q",
			got.Status, StatusCancelled)
	}
}

// TestManager_Cancel_KillFailed verifies a live process group is
// surfaced as kill_failed with its PID.
func TestManager_Cancel_KillFailed(t *testing.T) {
	proc := startGroupedSleep(t)

	rec := newMemRecorder()
	m := NewManager(rec)
	m.killWait = 50 * time.Millisecond

	h, err := m.Begin(context.Background(), New{
		Tool: "shell", Command: "sleep 30",
	})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}

	_ = h.Start(context.Background())
	h.SetPID(proc.Pid)

	err = m.Cancel(h.ID())

	var killErr *KillFailedError
	if !errors.As(err, &killErr) {
		t.Fatalf("Cancel error = %v, want KillFailedError", err)
	}
	if killErr.PID != proc.Pid {
		t.Errorf("PID = %d, want %d", killErr.PID, proc.Pid)
	}

	got := rec.get(h.ID())
	if got.Status != StatusKillFailed {
		t.Errorf("status = %q, want %q",
			got.Status, StatusKillFailed)
	}

	// A later Finish must not overwrite the terminal failure.
	h.Finish("", nil)
	if got = rec.get(h.ID()); got.Status != StatusKillFailed {
		t.Errorf("after Finish status = %q, want %q",
			got.Status, StatusKillFailed)
	}
}

// TestManager_Cancel_UnknownID verifies cancellation of a missing
// execution does not invent state.
func TestManager_Cancel_UnknownID(t *testing.T) {
	m := NewManager(newMemRecorder())

	err := m.Cancel("missing")
	if !errors.Is(err, ErrNotRunning) {
		t.Errorf("Cancel missing = %v, want ErrNotRunning", err)
	}
}

// TestHandle_Finish_Timeout verifies deadline errors map to
// timed_out.
func TestHandle_Finish_Timeout(t *testing.T) {
	rec := newMemRecorder()
	m := NewManager(rec)

	h, err := m.Begin(context.Background(), New{
		Tool: "shell", Command: "sleep 30",
	})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	_ = h.Start(context.Background())

	h.Finish("",
		fmt.Errorf("shell timed out: %w", context.DeadlineExceeded))

	got := rec.get(h.ID())
	if got.Status != StatusTimedOut {
		t.Errorf("status = %q, want %q",
			got.Status, StatusTimedOut)
	}
}

// TestHandle_Finish_Denied verifies a permission denial is recorded
// distinctly from a failed execution.
func TestHandle_Finish_Denied(t *testing.T) {
	rec := newMemRecorder()
	m := NewManager(rec)

	h, err := m.Begin(context.Background(), New{
		Tool: "shell", Command: "rm -rf /",
	})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}

	h.Finish("error: permission denied for shell: rm -rf /", nil)

	got := rec.get(h.ID())
	if got.Status != StatusDenied {
		t.Errorf("status = %q, want %q",
			got.Status, StatusDenied)
	}
}

// TestHandle_SetExitCode_Persists verifies tool exit codes reach the
// durable record.
func TestHandle_SetExitCode_Persists(t *testing.T) {
	rec := newMemRecorder()
	m := NewManager(rec)

	h, err := m.Begin(context.Background(), New{
		Tool: "shell", Command: "false",
	})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	_ = h.Start(context.Background())

	ctx := WithHandle(context.Background(), h)
	SetExitCode(ctx, 7)
	h.Finish("", nil)

	got := rec.get(h.ID())
	if got.ExitCode == nil || *got.ExitCode != 7 {
		t.Errorf("exit code = %v, want 7", got.ExitCode)
	}
}

// TestHandle_Finish_Success verifies a clean run becomes succeeded.
func TestHandle_Finish_Success(t *testing.T) {
	rec := newMemRecorder()
	m := NewManager(rec)

	h, err := m.Begin(context.Background(), New{
		Tool: "shell", Command: "echo hi",
	})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	_ = h.Start(context.Background())
	h.Finish("hi", nil)

	got := rec.get(h.ID())
	if got.Status != StatusSucceeded {
		t.Errorf("status = %q, want %q",
			got.Status, StatusSucceeded)
	}
	if got.OutputRef == "" {
		t.Error("output_ref is empty, want persisted output")
	}
}

// TestHandle_Finish_NonZeroExit verifies a reported exit code is
// stored without turning a completed command into a failure.
func TestHandle_Finish_NonZeroExit(t *testing.T) {
	rec := newMemRecorder()
	m := NewManager(rec)

	h, err := m.Begin(context.Background(), New{
		Tool: "shell", Command: "false",
	})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	ctx := h.Start(context.Background())
	SetExitCode(ctx, 2)
	h.Finish("exit code: 2", nil)

	got := rec.get(h.ID())
	if got.Status != StatusSucceeded {
		t.Errorf("status = %q, want %q",
			got.Status, StatusSucceeded)
	}
	if got.ExitCode == nil || *got.ExitCode != 2 {
		t.Errorf("exit code = %v, want 2", got.ExitCode)
	}
}

// TestHandle_Finish_Failure verifies a tool error becomes failed.
func TestHandle_Finish_Failure(t *testing.T) {
	rec := newMemRecorder()
	m := NewManager(rec)

	h, err := m.Begin(context.Background(), New{
		Tool: "shell", Command: "false",
	})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	_ = h.Start(context.Background())
	h.Finish("", errors.New("boom"))

	got := rec.get(h.ID())
	if got.Status != StatusFailed {
		t.Errorf("status = %q, want %q",
			got.Status, StatusFailed)
	}
	if !strings.Contains(got.Error, "boom") {
		t.Errorf("error = %q, want boom", got.Error)
	}
}

// TestManager_Output_Paginated verifies persisted output is returned
// in bounded pages until done.
func TestManager_Output_Paginated(t *testing.T) {
	rec := newMemRecorder()
	m := NewManager(rec)

	h, err := m.Begin(context.Background(), New{
		Tool: "shell", Command: "seq",
	})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	_ = h.Start(context.Background())

	want := strings.Repeat("x", OutputChunkBytes*2+17)
	h.Finish(want, nil)

	var (
		got     strings.Builder
		cursor  string
		pages   int
		maxPage int
	)

	for {
		chunk, err := m.Output(
			context.Background(), h.ID(), cursor,
		)
		if err != nil {
			t.Fatalf("Output: %v", err)
		}
		got.WriteString(chunk.Data)
		pages++
		if len(chunk.Data) > maxPage {
			maxPage = len(chunk.Data)
		}
		if chunk.Done {
			break
		}
		cursor = chunk.Next
	}

	if got.String() != want {
		t.Errorf("output length = %d, want %d",
			got.Len(), len(want))
	}
	if pages < 3 {
		t.Errorf("pages = %d, want at least 3", pages)
	}
	if maxPage > OutputChunkBytes {
		t.Errorf("page size = %d, want <= %d",
			maxPage, OutputChunkBytes)
	}
}
