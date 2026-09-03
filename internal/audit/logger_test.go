package audit

import (
	"sync"
	"testing"
	"time"
)

func TestLogger_BufferFlush(t *testing.T) {
	store := newTestStore(t)
	logger := NewLogger(store, 100, 100*time.Millisecond, 65536)
	defer logger.Close()

	logger.LogToolRun(AuditEntry{
		SessionID: "flush-test",
		Tool:      "shell",
		Args:      "{}",
	}, nil)

	time.Sleep(300 * time.Millisecond)

	entries, err := store.QueryEntries(QueryFilter{SessionID: "flush-test"})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 entry after flush, got %d", len(entries))
	}
}

func TestLogger_BatchFlush(t *testing.T) {
	store := newTestStore(t)
	logger := NewLogger(store, 200, 1*time.Hour, 65536)
	defer logger.Close()

	for i := 0; i < 100; i++ {
		logger.LogToolRun(AuditEntry{
			SessionID: "batch-test",
			Tool:      "shell",
			Args:      "{}",
		}, nil)
	}

	time.Sleep(200 * time.Millisecond)

	entries, err := store.QueryEntries(QueryFilter{
		SessionID: "batch-test",
		Limit:     200,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 100 {
		t.Errorf("expected 100 entries after batch flush, got %d", len(entries))
	}
}

func TestLogger_Shutdown(t *testing.T) {
	store := newTestStore(t)
	logger := NewLogger(store, 100, 1*time.Hour, 65536)

	for i := 0; i < 5; i++ {
		logger.LogToolRun(AuditEntry{
			SessionID: "shutdown-test",
			Tool:      "shell",
			Args:      "{}",
		}, nil)
	}

	logger.Close()

	entries, err := store.QueryEntries(QueryFilter{
		SessionID: "shutdown-test",
		Limit:     10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 5 {
		t.Errorf("expected 5 entries after shutdown, got %d", len(entries))
	}
}

func TestLogger_Blocking(t *testing.T) {
	store := newTestStore(t)
	logger := NewLogger(store, 2, 1*time.Hour, 65536)
	defer logger.Close()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 10; i++ {
			logger.LogToolRun(AuditEntry{
				SessionID: "block-test",
				Tool:      "shell",
				Args:      "{}",
			}, nil)
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Error("LogToolRun should not block on full buffer")
	}
}

func TestLogger_Concurrent(t *testing.T) {
	store := newTestStore(t)
	logger := NewLogger(store, 1000, 100*time.Millisecond, 65536)
	defer logger.Close()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				logger.LogToolRun(AuditEntry{
					SessionID: "conc-logger",
					Tool:      "shell",
					Args:      "{}",
				}, nil)
			}
		}(i)
	}
	wg.Wait()

	time.Sleep(300 * time.Millisecond)

	entries, err := store.QueryEntries(QueryFilter{
		SessionID: "conc-logger",
		Limit:     1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 500 {
		t.Errorf("expected 500 entries, got %d", len(entries))
	}
}

func TestLogger_PermissionFlush(t *testing.T) {
	store := newTestStore(t)
	logger := NewLogger(store, 100, 100*time.Millisecond, 65536)
	defer logger.Close()

	logger.LogPermission(PermissionAudit{
		SessionID: "perm-test",
		Tool:      "shell",
		Command:   "ls",
		Decision:  "allow",
	})

	time.Sleep(300 * time.Millisecond)

	perms, err := store.QueryPermissions(QueryFilter{SessionID: "perm-test"})
	if err != nil {
		t.Fatal(err)
	}
	if len(perms) != 1 {
		t.Errorf("expected 1 permission, got %d", len(perms))
	}
}

func TestLogger_SubagentFlush(t *testing.T) {
	store := newTestStore(t)
	logger := NewLogger(store, 100, 100*time.Millisecond, 65536)
	defer logger.Close()

	logger.LogSubagent(SubagentAudit{
		ParentSessionID: "parent",
		ChildSessionID:  "child",
		Tool:            "task",
		Status:          "running",
	})

	time.Sleep(300 * time.Millisecond)

	subs, err := store.GetSubagents("parent")
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 1 {
		t.Errorf("expected 1 subagent, got %d", len(subs))
	}

	logger.CompleteSubagent("child", 1000, "completed")
	time.Sleep(300 * time.Millisecond)

	subs, err = store.GetSubagents("parent")
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 1 {
		t.Fatalf("expected 1 subagent, got %d", len(subs))
	}
	if subs[0].Status != "completed" {
		t.Errorf("expected status completed, got %s", subs[0].Status)
	}
}

func TestLogger_ToolRunWithOutput(t *testing.T) {
	store := newTestStore(t)
	logger := NewLogger(store, 100, 100*time.Millisecond, 65536)
	defer logger.Close()

	logger.LogToolRun(
		AuditEntry{
			SessionID: "output-fk-test",
			Tool:      "shell",
			Args:      `{"command":"echo hello"}`,
			ExitCode:  intPtr(0),
		},
		&OutputCapture{
			SessionID: "output-fk-test",
			Stdout:    "hello\n",
			Stderr:    "",
		},
	)

	time.Sleep(300 * time.Millisecond)

	entries, err := store.QueryEntries(QueryFilter{SessionID: "output-fk-test"})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	output, err := store.GetOutput(entries[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if output == nil {
		t.Fatal("expected output, got nil")
	}
	if output.EntryID != entries[0].ID {
		t.Errorf("output EntryID: want %d, got %d", entries[0].ID, output.EntryID)
	}
	if output.Stdout != "hello\n" {
		t.Errorf("expected stdout 'hello\\n', got %q", output.Stdout)
	}
}

func TestTruncateOutput(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxBytes int
		want     string
	}{
		{"no limit", "hello", 0, "hello"},
		{"under limit", "hello", 10, "hello"},
		{"exact limit", "hello", 5, "hello"},
		{"over limit", "hello world", 5, "hello"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateOutput(tt.input, tt.maxBytes)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
