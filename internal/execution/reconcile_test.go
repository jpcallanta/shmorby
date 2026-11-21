package execution

import (
	"testing"
	"time"
)

// TestReconcile_StaleRunning_MarksUnknownOrOrphaned verifies startup
// recovery never leaves stale rows running and never marks success.
func TestReconcile_StaleRunning_MarksUnknownOrOrphaned(t *testing.T) {
	proc := startGroupedSleep(t)

	rec := newMemRecorder()
	now := time.Now().UTC()
	rows := []Record{
		{ID: "unknown", Status: StatusRunning, CreatedAt: now},
		{ID: "orphan", Status: StatusRunning, PID: proc.Pid,
			ProcessStart: ProcessIdentity(proc.Pid),
			CreatedAt:    now},
		{ID: "recycled", Status: StatusRunning, PID: proc.Pid,
			ProcessStart: "stale-process-identity",
			CreatedAt:    now},
		{ID: "old-live", Status: StatusOrphaned, PID: proc.Pid,
			ProcessStart: ProcessIdentity(proc.Pid),
			CreatedAt:    now},
		{ID: "old-dead", Status: StatusOrphaned, CreatedAt: now},
		{ID: "pending", Status: StatusPendingApproval, CreatedAt: now},
		{ID: "cancel", Status: StatusCancelRequested, CreatedAt: now},
		{ID: "done", Status: StatusSucceeded, CreatedAt: now},
	}
	for _, r := range rows {
		if err := rec.CreateExecution(r); err != nil {
			t.Fatalf("CreateExecution: %v", err)
		}
	}

	got, err := Reconcile(rec, now)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(got) != 7 {
		t.Fatalf("reconciled %d rows, want 7", len(got))
	}

	statuses := map[string]Status{}
	for _, r := range got {
		statuses[r.ID] = r.Status
	}
	if statuses["unknown"] != StatusUnknown {
		t.Errorf("unknown status = %q, want %q",
			statuses["unknown"], StatusUnknown)
	}
	if statuses["orphan"] != StatusOrphaned {
		t.Errorf("orphan status = %q, want %q",
			statuses["orphan"], StatusOrphaned)
	}
	if statuses["recycled"] != StatusUnknown {
		t.Errorf("recycled status = %q, want %q",
			statuses["recycled"], StatusUnknown)
	}
	if statuses["old-live"] != StatusOrphaned {
		t.Errorf("old-live status = %q, want %q",
			statuses["old-live"], StatusOrphaned)
	}
	if statuses["old-dead"] != StatusUnknown {
		t.Errorf("old-dead status = %q, want %q",
			statuses["old-dead"], StatusUnknown)
	}
	if statuses["pending"] != StatusUnknown {
		t.Errorf("pending status = %q, want %q",
			statuses["pending"], StatusUnknown)
	}
	if statuses["cancel"] != StatusUnknown {
		t.Errorf("cancel status = %q, want %q",
			statuses["cancel"], StatusUnknown)
	}
	if got := rec.get("done"); got.Status != StatusSucceeded {
		t.Errorf("done status = %q, want %q",
			got.Status, StatusSucceeded)
	}
}
