package session

import (
	"errors"
	"strings"
	"testing"
	"time"

	"shmorby/internal/execution"
)

// TestStore_ExecutionLifecycle verifies create, update, and filtered
// listing against the session database.
func TestStore_ExecutionLifecycle(t *testing.T) {
	st := newTestStore(t)
	now := time.Now().UTC()

	rec := execution.Record{
		ID:         "exec-1",
		SessionID:  "s1",
		ToolCallID: "call-1",
		Tool:       "shell",
		Command:    "df -h",
		ArgsHash:   "hash",
		Status:     execution.StatusRunning,
		PID:        1234,
		CreatedAt:  now,
		StartedAt:  now,
	}
	if err := st.CreateExecution(rec); err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}

	exit := 0
	rec.Status = execution.StatusSucceeded
	rec.EndedAt = now.Add(time.Second)
	rec.ExitCode = &exit
	if err := st.UpdateExecution(rec); err != nil {
		t.Fatalf("UpdateExecution: %v", err)
	}

	got, err := st.ListExecutions(execution.Filter{
		SessionID: "s1",
		Statuses:  []execution.Status{execution.StatusSucceeded},
	})
	if err != nil {
		t.Fatalf("ListExecutions: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("listed %d executions, want 1", len(got))
	}
	if got[0].Status != execution.StatusSucceeded {
		t.Errorf("status = %q, want %q",
			got[0].Status, execution.StatusSucceeded)
	}
	if got[0].ExitCode == nil || *got[0].ExitCode != 0 {
		t.Errorf("exit code = %v, want 0", got[0].ExitCode)
	}

	missing := rec
	missing.ID = "missing"
	if err := st.UpdateExecution(missing); err == nil {
		t.Error("UpdateExecution missing returned nil")
	}
}

// TestStore_ExecutionRedactsSecrets verifies commands and errors are
// redacted at the persistence boundary.
func TestStore_ExecutionRedactsSecrets(t *testing.T) {
	st := newTestStore(t)

	rec := execution.Record{
		ID:        "exec-secret",
		SessionID: "s1",
		Tool:      "shell",
		Command:   "export KEY=AKIA1234567890ABCDEF",
		Status:    execution.StatusFailed,
		Error:     "failed with AKIA1234567890ABCDEF",
		CreatedAt: time.Now().UTC(),
	}
	if err := st.CreateExecution(rec); err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}

	rows, err := st.ListExecutions(execution.Filter{SessionID: "s1"})
	if err != nil {
		t.Fatalf("ListExecutions: %v", err)
	}
	got := rows[0]
	if strings.Contains(got.Command, "AKIA1234567890ABCDEF") {
		t.Errorf("command leaked secret: %q", got.Command)
	}
	if strings.Contains(got.Error, "AKIA1234567890ABCDEF") {
		t.Errorf("error leaked secret: %q", got.Error)
	}
}

// TestStore_DeleteCascadesExecutions verifies hard-deleting a session
// and its children removes their execution rows.
func TestStore_DeleteCascadesExecutions(t *testing.T) {
	st := newTestStore(t)
	mustSave(t, st, "parent")
	if err := st.Save(Meta{ID: "child", ParentID: "parent"}); err != nil {
		t.Fatalf("Save child: %v", err)
	}

	for _, id := range []string{"parent", "child"} {
		rec := execution.Record{
			ID:        "exec-" + id,
			SessionID: id,
			Tool:      "shell",
			Status:    execution.StatusSucceeded,
			CreatedAt: time.Now().UTC(),
		}
		if err := st.CreateExecution(rec); err != nil {
			t.Fatalf("CreateExecution %s: %v", id, err)
		}
	}

	if err := st.Delete("parent"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	rows, err := st.ListExecutions(execution.Filter{})
	if err != nil {
		t.Fatalf("ListExecutions: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("executions after delete = %d, want 0", len(rows))
	}
}

// TestStore_GetExecution verifies correlation fields round-trip.
func TestStore_GetExecution(t *testing.T) {
	st := newTestStore(t)
	rec := execution.Record{
		ID:           "exec-get",
		SessionID:    "s1",
		MessageID:    "msg-1",
		ToolCallID:   "call-1",
		Tool:         "shell",
		Command:      "echo hi",
		ArgsRedacted: `{"command":"echo hi"}`,
		ArgsHash:     "hash",
		Status:       execution.StatusSucceeded,
		CreatedAt:    time.Now().UTC(),
	}
	if err := st.CreateExecution(rec); err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}

	got, err := st.GetExecution(rec.ID)
	if err != nil {
		t.Fatalf("GetExecution: %v", err)
	}
	if got.MessageID != rec.MessageID ||
		got.ArgsRedacted != rec.ArgsRedacted {
		t.Errorf("record = %+v, want %+v", got, rec)
	}

	if _, err := st.GetExecution("missing"); !errors.Is(
		err, execution.ErrNotFound,
	) {
		t.Errorf("GetExecution missing = %v, want ErrNotFound", err)
	}
}

// TestStore_ExecutionOutput_Paginated verifies output chunks survive
// a store round trip and are bounded by the caller.
func TestStore_ExecutionOutput_Paginated(t *testing.T) {
	st := newTestStore(t)
	rec := execution.Record{
		ID:        "exec-out",
		SessionID: "s1",
		Tool:      "shell",
		Status:    execution.StatusSucceeded,
		CreatedAt: time.Now().UTC(),
	}
	if err := st.CreateExecution(rec); err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}
	if err := st.AppendExecutionOutput(
		rec.ID, []string{"first", "second"},
	); err != nil {
		t.Fatalf("AppendExecutionOutput: %v", err)
	}

	data, done, err := st.ReadExecutionOutput(rec.ID, 0)
	if err != nil {
		t.Fatalf("ReadExecutionOutput 0: %v", err)
	}
	if data != "first" || done {
		t.Errorf("chunk 0 = %q, done = %v", data, done)
	}

	data, done, err = st.ReadExecutionOutput(rec.ID, 1)
	if err != nil {
		t.Fatalf("ReadExecutionOutput 1: %v", err)
	}
	if data != "second" || !done {
		t.Errorf("chunk 1 = %q, done = %v", data, done)
	}

	data, done, err = st.ReadExecutionOutput(rec.ID, 2)
	if err != nil {
		t.Fatalf("ReadExecutionOutput 2: %v", err)
	}
	if data != "" || !done {
		t.Errorf("past end = %q, done = %v", data, done)
	}
}

// TestStore_ExecutionOutputRedactsSecrets verifies output is redacted
// at the persistence boundary.
func TestStore_ExecutionOutputRedactsSecrets(t *testing.T) {
	st := newTestStore(t)
	rec := execution.Record{
		ID:        "exec-secret-out",
		SessionID: "s1",
		Tool:      "shell",
		Status:    execution.StatusSucceeded,
		CreatedAt: time.Now().UTC(),
	}
	if err := st.CreateExecution(rec); err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}

	secret := "AKIA1234567890ABCDEF"
	if err := st.AppendExecutionOutput(
		rec.ID, []string{"token=" + secret},
	); err != nil {
		t.Fatalf("AppendExecutionOutput: %v", err)
	}

	data, _, err := st.ReadExecutionOutput(rec.ID, 0)
	if err != nil {
		t.Fatalf("ReadExecutionOutput: %v", err)
	}
	if strings.Contains(data, secret) {
		t.Errorf("output leaked secret: %q", data)
	}
}
