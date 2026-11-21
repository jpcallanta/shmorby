package approval

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"shmorby/internal/execution"
	"shmorby/internal/session"
)

func TestServiceRequest_PersistsDecisions(t *testing.T) {
	tests := []struct {
		name       string
		decision   Decision
		status     string
		execStatus execution.Status
		scope      string
	}{
		{
			name:       "once",
			decision:   DecisionOnce,
			status:     "approved",
			execStatus: execution.StatusRunning,
			scope:      "call",
		},
		{
			name:       "deny",
			decision:   DecisionReject,
			status:     "denied",
			execStatus: execution.StatusDenied,
			scope:      "call",
		},
		{
			name:       "allow all",
			decision:   DecisionAlways,
			status:     "approved",
			execStatus: execution.StatusRunning,
			scope:      "turn",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newTestStore(t)
			execID := "exec-" + tt.name
			if err := st.CreateExecution(execution.Record{
				ID:        execID,
				SessionID: "session-1",
				Tool:      "shell",
				Status:    execution.StatusPendingApproval,
				CreatedAt: time.Now().UTC(),
			}); err != nil {
				t.Fatalf("CreateExecution: %v", err)
			}

			service := NewService(st, time.Second)
			got := service.Request(
				context.Background(),
				session.ApprovalRecord{
					SessionID:       "session-1",
					ExecutionID:     execID,
					Tool:            "shell",
					CommandRedacted: "api_key=abcdefghij1234567890",
					RulePattern:     "rm *",
					RuleAction:      "ask",
					Reason:          "matched rule",
				},
				func(session.ApprovalRecord) Prompt {
					ch := make(chan Decision, 1)
					ch <- tt.decision
					return Prompt{Decisions: ch}
				},
			)
			if got.Decision != tt.decision {
				t.Fatalf("decision = %q, want %q", got.Decision, tt.decision)
			}
			if got.DecidedBy != LocalIdentity() ||
				got.DecisionSource != "interactive" {
				t.Errorf("result attribution = %+v, want local interactive",
					got)
			}

			rec, err := st.GetExecution(execID)
			if err != nil {
				t.Fatalf("GetExecution: %v", err)
			}
			id := rec.ApprovalID
			if rec.Status != tt.execStatus {
				t.Errorf("execution status = %q, want %q",
					rec.Status, tt.execStatus)
			}
			if id == "" {
				t.Fatal("execution has no linked approval ID")
			}
			a, err := st.(interface {
				GetApproval(string) (session.ApprovalRecord, error)
			}).GetApproval(id)
			if err != nil {
				t.Fatalf("GetApproval: %v", err)
			}
			if a.Status != tt.status || a.Decision != string(tt.decision) {
				t.Errorf("approval = %+v, want status %q decision %q",
					a, tt.status, tt.decision)
			}
			if a.Scope != tt.scope {
				t.Errorf("scope = %q, want %q", a.Scope, tt.scope)
			}
			if a.DecidedBy != LocalIdentity() ||
				a.DecisionSource != "interactive" {
				t.Errorf("decision identity = %q/%q, want local user/interactive",
					a.DecidedBy, a.DecisionSource)
			}
			if strings.Contains(a.CommandRedacted, "abcdefghij1234567890") {
				t.Error("approval command persisted an unredacted token")
			}
			if a.DecidedAt.IsZero() || a.ExpiresAt.IsZero() {
				t.Errorf("approval timestamps missing: %+v", a)
			}
		})
	}
}

func TestServiceRequest_TimeoutExpiresAndDenies(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateExecution(execution.Record{
		ID:        "exec-timeout",
		SessionID: "session-1",
		Tool:      "shell",
		Status:    execution.StatusPendingApproval,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}

	expired := make(chan struct{}, 1)
	service := NewService(st, 10*time.Millisecond)
	got := service.Request(
		context.Background(),
		session.ApprovalRecord{
			SessionID: "session-1", ExecutionID: "exec-timeout",
			Tool: "shell", CommandRedacted: "reboot",
		},
		func(session.ApprovalRecord) Prompt {
			return Prompt{OnExpire: func() { expired <- struct{}{} }}
		},
	)
	if got.Decision != DecisionReject {
		t.Fatalf("decision = %q, want rejection", got.Decision)
	}
	if got.DecidedBy != "system" || got.DecisionSource != "timeout" {
		t.Errorf("timeout attribution = %+v, want system timeout", got)
	}

	exec, err := st.GetExecution("exec-timeout")
	if err != nil {
		t.Fatalf("GetExecution: %v", err)
	}
	if exec.Status != execution.StatusDenied {
		t.Errorf("execution status = %q, want denied", exec.Status)
	}
	a, err := st.(interface {
		GetApproval(string) (session.ApprovalRecord, error)
	}).GetApproval(exec.ApprovalID)
	if err != nil {
		t.Fatalf("GetApproval: %v", err)
	}
	if a.Status != "expired" || a.Decision != string(DecisionReject) {
		t.Errorf("approval = %+v, want expired rejection", a)
	}
	if a.DecisionSource != "timeout" {
		t.Errorf("decision source = %q, want timeout", a.DecisionSource)
	}
	select {
	case <-expired:
	default:
		t.Error("timeout callback was not called")
	}
}

func TestStore_StartupExpiresPendingApprovals(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	st, err := session.NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.CreateExecution(execution.Record{
		ID:        "exec-restart",
		SessionID: "session-1",
		Tool:      "shell",
		Status:    execution.StatusPendingApproval,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}
	if err := st.CreateApproval(session.ApprovalRecord{
		ID: "approval-restart", SessionID: "session-1",
		ExecutionID: "exec-restart", Tool: "shell",
		CommandRedacted: "reboot", Status: "pending",
		RequestedAt: time.Now().UTC(),
		ExpiresAt:   time.Now().Add(time.Hour).UTC(),
	}); err != nil {
		t.Fatalf("CreateApproval: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Close before restart: %v", err)
	}
	st, err = session.NewStore(path)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}

	now := time.Now().UTC()
	if err := st.ExpirePendingApprovals(now); err != nil {
		t.Fatalf("ExpirePendingApprovals: %v", err)
	}
	a, err := st.(interface {
		GetApproval(string) (session.ApprovalRecord, error)
	}).GetApproval("approval-restart")
	if err != nil {
		t.Fatalf("GetApproval: %v", err)
	}
	if a.Status != "expired" || a.Decision != string(DecisionReject) {
		t.Errorf("approval = %+v, want expired rejection", a)
	}
	if a.DecisionSource != "restart" || a.DecidedBy != "system" {
		t.Errorf("recovery attribution = %q/%q, want system/restart",
			a.DecidedBy, a.DecisionSource)
	}
	exec, err := st.GetExecution("exec-restart")
	if err != nil {
		t.Fatalf("GetExecution: %v", err)
	}
	if exec.Status != execution.StatusDenied {
		t.Errorf("execution status = %q, want denied", exec.Status)
	}
}

// TestStore_StartupKeepsLiveOwnersPendingApprovals checks startup recovery
// leaves requests belonging to live processes untouched.
func TestStore_StartupKeepsLiveOwnersPendingApprovals(t *testing.T) {
	st := newTestStore(t)
	ownerPID := os.Getpid()
	ownerStart := execution.ProcessIdentity(ownerPID)
	if ownerStart == "" {
		t.Fatal("current process identity is unavailable")
	}
	if err := st.CreateExecution(execution.Record{
		ID: "exec-live-owner", SessionID: "session-1", Tool: "shell",
		Status: execution.StatusPendingApproval, OwnerPID: ownerPID,
		OwnerStart: ownerStart, CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}
	if err := st.CreateApproval(session.ApprovalRecord{
		ID: "approval-live-owner", SessionID: "session-1",
		ExecutionID: "exec-live-owner", Tool: "shell",
		CommandRedacted: "reboot", Status: "pending",
		RequestedAt: time.Now().UTC(),
		ExpiresAt:   time.Now().Add(time.Hour).UTC(),
	}); err != nil {
		t.Fatalf("CreateApproval: %v", err)
	}

	if err := st.ExpirePendingApprovals(time.Now().UTC()); err != nil {
		t.Fatalf("ExpirePendingApprovals: %v", err)
	}
	a, err := st.(interface {
		GetApproval(string) (session.ApprovalRecord, error)
	}).GetApproval("approval-live-owner")
	if err != nil {
		t.Fatalf("GetApproval: %v", err)
	}
	if a.Status != "pending" {
		t.Errorf("approval status = %q, want pending", a.Status)
	}
	exec, err := st.GetExecution("exec-live-owner")
	if err != nil {
		t.Fatalf("GetExecution: %v", err)
	}
	if exec.Status != execution.StatusPendingApproval {
		t.Errorf("execution status = %q, want pending approval", exec.Status)
	}
}

func newTestStore(t *testing.T) session.Store {
	t.Helper()

	st, err := session.NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	return st
}

// TestStore_RestartResolvesPendingApprovalLinksExecution checks a
// request persisted before a restart can be decided afterwards and the
// decision is linked to its execution.
func TestStore_RestartResolvesPendingApprovalLinksExecution(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	st, err := session.NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.CreateExecution(execution.Record{
		ID:        "exec-restart",
		SessionID: "session-1",
		Tool:      "shell",
		Status:    execution.StatusPendingApproval,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}
	if err := st.CreateApproval(session.ApprovalRecord{
		ID: "approval-restart", SessionID: "session-1",
		ExecutionID: "exec-restart", Tool: "shell",
		CommandRedacted: "reboot", Status: "pending",
		RequestedAt: time.Now().UTC(),
		ExpiresAt:   time.Now().Add(time.Hour).UTC(),
	}); err != nil {
		t.Fatalf("CreateApproval: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Close before restart: %v", err)
	}

	st, err = session.NewStore(path)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	approvals := st.(interface {
		GetApproval(string) (session.ApprovalRecord, error)
	})
	a, err := approvals.GetApproval("approval-restart")
	if err != nil {
		t.Fatalf("GetApproval: %v", err)
	}
	a.Decision = string(DecisionOnce)
	a.Status = "approved"
	a.DecidedBy = "tester"
	a.DecisionSource = "interactive"
	a.Scope = "call"
	a.DecidedAt = time.Now().UTC()
	if err := st.ResolveApproval(a); err != nil {
		t.Fatalf("ResolveApproval: %v", err)
	}

	got, err := approvals.GetApproval("approval-restart")
	if err != nil {
		t.Fatalf("GetApproval after resolve: %v", err)
	}
	if got.Status != "approved" || got.Decision != string(DecisionOnce) {
		t.Errorf("approval = %+v, want approved once", got)
	}
	exec, err := st.GetExecution("exec-restart")
	if err != nil {
		t.Fatalf("GetExecution: %v", err)
	}
	if exec.Status != execution.StatusRunning {
		t.Errorf("execution status = %q, want running", exec.Status)
	}
}
