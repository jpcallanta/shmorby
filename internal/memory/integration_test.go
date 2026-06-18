//go:build integration

package memory

import (
	"path/filepath"
	"testing"
	"time"
)

// Tests the full insert → search → delete cycle with a real SQLite database.
func TestMemoryIntegration(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(Config{
		Enabled: true,
		DBPath:  filepath.Join(dir, "test.db"),
	}, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	entry := MemoryEntry{
		ID:        "integ-test-1",
		Timestamp: time.Now(),
		SessionID: "session-1",
		Tool:      "shell",
		Command:   "echo hello",
		Result:    "hello",
		ExitCode:  0,
		Summary:   "test entry",
		Tags:      []string{"test", "integration"},
	}

	if err := store.Insert(entry); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	got, err := store.Get("integ-test-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Command != "echo hello" {
		t.Errorf("want command 'echo hello', got %q", got.Command)
	}

	list, err := store.List(10, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("want 1 entry in List, got %d", len(list))
	}

	// Use retriever for keyword fallback search.
	r := NewRetriever(store, 10)
	result, err := r.Retrieve(t.Context(), "hello")
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(result.Entries) == 0 {
		t.Error("Retrieve should find 'hello'")
	}

	count, err := store.Count()
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 1 {
		t.Errorf("want count 1, got %d", count)
	}

	if err := store.Delete("integ-test-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	count, err = store.Count()
	if err != nil {
		t.Fatalf("Count after delete: %v", err)
	}
	if count != 0 {
		t.Errorf("want count 0 after delete, got %d", count)
	}
}
