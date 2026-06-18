package memory

import (
	"context"
	"testing"

	chromem "github.com/philippgille/chromem-go"
)

// mockStore is a minimal Store implementation for testing stats without FTS5.
type mockStore struct {
	entries []MemoryEntry
}

func (m *mockStore) Insert(entry MemoryEntry) error {
	m.entries = append(m.entries, entry)
	return nil
}

func (m *mockStore) Get(id string) (MemoryEntry, error) {
	for _, e := range m.entries {
		if e.ID == id {
			return e, nil
		}
	}
	return MemoryEntry{}, nil
}

func (m *mockStore) Delete(id string) error {
	for i, e := range m.entries {
		if e.ID == id {
			m.entries = append(m.entries[:i], m.entries[i+1:]...)
			return nil
		}
	}
	return nil
}

func (m *mockStore) List(limit, offset int) ([]MemoryEntry, error) {
	if offset >= len(m.entries) {
		return nil, nil
	}
	end := offset + limit
	if end > len(m.entries) {
		end = len(m.entries)
	}
	return m.entries[offset:end], nil
}

func (m *mockStore) Count() (int, error) {
	return len(m.entries), nil
}

func (m *mockStore) Close() error {
	return nil
}

func (m *mockStore) AutoCaptureEnabled() bool {
	return true
}

func (m *mockStore) TagRules() []TagRule {
	return nil
}

// TestRetriever_Stats_InitialState checks stats start at zero.
func TestRetriever_Stats_InitialState(t *testing.T) {
	r := NewRetriever(&mockStore{}, 5)

	stats := r.Stats()
	if stats.Hits != 0 {
		t.Errorf("Hits: want 0, got %d", stats.Hits)
	}
	if stats.Misses != 0 {
		t.Errorf("Misses: want 0, got %d", stats.Misses)
	}
	if stats.LastHit {
		t.Errorf("LastHit: want false, got true")
	}
	if stats.LastCount != 0 {
		t.Errorf("LastCount: want 0, got %d", stats.LastCount)
	}
	if stats.LastQuery != "" {
		t.Errorf("LastQuery: want empty, got %q", stats.LastQuery)
	}
}

// TestRetriever_Total_InitialState checks total starts at zero.
func TestRetriever_Total_InitialState(t *testing.T) {
	r := NewRetriever(&mockStore{}, 5)

	if r.Total() != 0 {
		t.Errorf("Total: want 0, got %d", r.Total())
	}
}

// TestRetriever_Retrieve_HitSetsTrue checks Hit is true when entries found.
func TestRetriever_Retrieve_HitSetsTrue(t *testing.T) {
	ms := &mockStore{
		entries: []MemoryEntry{
			{ID: "1", Command: "echo hello", Result: "hello"},
		},
	}
	r := NewRetriever(ms, 5)

	result, err := r.Retrieve(context.Background(), "echo")
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}

	if !result.Hit {
		t.Error("Hit: want true, got false")
	}
	if len(result.Entries) != 1 {
		t.Errorf("Entries: want 1, got %d", len(result.Entries))
	}
}

// TestRetriever_Retrieve_MissSetsFalse checks Hit is false when no entries.
func TestRetriever_Retrieve_MissSetsFalse(t *testing.T) {
	ms := &mockStore{
		entries: []MemoryEntry{
			{ID: "1", Command: "echo hello", Result: "hello"},
		},
	}
	r := NewRetriever(ms, 5)

	result, err := r.Retrieve(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}

	if result.Hit {
		t.Error("Hit: want false, got true")
	}
	if len(result.Entries) != 0 {
		t.Errorf("Entries: want 0, got %d", len(result.Entries))
	}
}

// TestRetriever_Stats_IncrementsHits checks hits counter increments.
func TestRetriever_Stats_IncrementsHits(t *testing.T) {
	ms := &mockStore{
		entries: []MemoryEntry{
			{ID: "1", Command: "echo hello", Result: "hello"},
		},
	}
	r := NewRetriever(ms, 5)

	r.Retrieve(context.Background(), "echo")
	r.Retrieve(context.Background(), "echo")

	stats := r.Stats()
	if stats.Hits != 2 {
		t.Errorf("Hits: want 2, got %d", stats.Hits)
	}
	if r.Total() != 2 {
		t.Errorf("Total: want 2, got %d", r.Total())
	}
}

// TestRetriever_Stats_IncrementsMisses checks misses counter increments.
func TestRetriever_Stats_IncrementsMisses(t *testing.T) {
	ms := &mockStore{
		entries: []MemoryEntry{
			{ID: "1", Command: "echo hello", Result: "hello"},
		},
	}
	r := NewRetriever(ms, 5)

	r.Retrieve(context.Background(), "nonexistent")
	r.Retrieve(context.Background(), "nonexistent")

	stats := r.Stats()
	if stats.Misses != 2 {
		t.Errorf("Misses: want 2, got %d", stats.Misses)
	}
	if r.Total() != 2 {
		t.Errorf("Total: want 2, got %d", r.Total())
	}
}

// TestRetriever_Stats_MixedHitsAndMisses checks both counters.
func TestRetriever_Stats_MixedHitsAndMisses(t *testing.T) {
	ms := &mockStore{
		entries: []MemoryEntry{
			{ID: "1", Command: "echo hello", Result: "hello"},
		},
	}
	r := NewRetriever(ms, 5)

	r.Retrieve(context.Background(), "echo")     // hit
	r.Retrieve(context.Background(), "nonexist") // miss
	r.Retrieve(context.Background(), "echo")     // hit

	stats := r.Stats()
	if stats.Hits != 2 {
		t.Errorf("Hits: want 2, got %d", stats.Hits)
	}
	if stats.Misses != 1 {
		t.Errorf("Misses: want 1, got %d", stats.Misses)
	}
	if r.Total() != 3 {
		t.Errorf("Total: want 3, got %d", r.Total())
	}
}

// TestRetriever_Stats_LastResult checks lastResult tracking.
func TestRetriever_Stats_LastResult(t *testing.T) {
	ms := &mockStore{
		entries: []MemoryEntry{
			{ID: "1", Command: "echo hello", Result: "hello"},
		},
	}
	r := NewRetriever(ms, 5)

	// First retrieve (hit)
	r.Retrieve(context.Background(), "echo")
	stats := r.Stats()
	if !stats.LastHit {
		t.Error("LastHit after hit: want true, got false")
	}
	if stats.LastCount != 1 {
		t.Errorf("LastCount after hit: want 1, got %d", stats.LastCount)
	}
	if stats.LastQuery != "echo" {
		t.Errorf("LastQuery after hit: want %q, got %q", "echo", stats.LastQuery)
	}

	// Second retrieve (miss)
	r.Retrieve(context.Background(), "nonexistent")
	stats = r.Stats()
	if stats.LastHit {
		t.Error("LastHit after miss: want false, got true")
	}
	if stats.LastCount != 0 {
		t.Errorf("LastCount after miss: want 0, got %d", stats.LastCount)
	}
	if stats.LastQuery != "nonexistent" {
		t.Errorf("LastQuery after miss: want %q, got %q", "nonexistent", stats.LastQuery)
	}
}

// TestRetriever_Retrieve_EmptyQuery checks empty query returns miss.
func TestRetriever_Retrieve_EmptyQuery_Stats(t *testing.T) {
	ms := &mockStore{
		entries: []MemoryEntry{
			{ID: "1", Command: "echo hello", Result: "hello"},
		},
	}
	r := NewRetriever(ms, 5)

	result, err := r.Retrieve(context.Background(), "")
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}

	if result.Hit {
		t.Error("Hit for empty query: want false, got true")
	}
	if len(result.Entries) != 0 {
		t.Errorf("Entries: want 0, got %d", len(result.Entries))
	}

	stats := r.Stats()
	if stats.Misses != 1 {
		t.Errorf("Misses: want 1, got %d", stats.Misses)
	}
}

// TestRetriever_NewRetriever_DefaultTopK ensures topK defaults to 5.
func TestRetriever_NewRetriever_DefaultTopK(t *testing.T) {
	r := NewRetriever(nil, 0)

	if r.topK != 5 {
		t.Errorf("topK: want 5, got %d", r.topK)
	}
}

// TestRetriever_NewRetriever_NegativeTopK defaults to 5.
func TestRetriever_NewRetriever_NegativeTopK(t *testing.T) {
	r := NewRetriever(nil, -1)

	if r.topK != 5 {
		t.Errorf("topK: want 5, got %d", r.topK)
	}
}

// retrievalEmbed returns deterministic embeddings for retriever tests.
func retrievalEmbed(_ context.Context, text string) ([]float32, error) {
	emb := make([]float32, 8)
	for i := range emb {
		emb[i] = float32(len(text)-i) * 0.001
	}

	return emb, nil
}

// TestRetriever_Retrieve_VectorSearch verifies the retriever uses
// vector search when SetVectorSearch is called.
func TestRetriever_Retrieve_VectorSearch(t *testing.T) {
	s := newTestSQLiteStore(t)
	ctx := context.Background()

	// Insert entries into SQLite.
	entries := []MemoryEntry{
		{ID: "v1", SessionID: "s1", Tool: "shell",
			Command: "systemctl restart nginx", Result: "ok"},
		{ID: "v2", SessionID: "s1", Tool: "shell",
			Command: "apt install nginx", Result: "done"},
		{ID: "v3", SessionID: "s1", Tool: "shell",
			Command: "ls -la", Result: "files"},
	}
	for _, e := range entries {
		if err := s.Insert(e); err != nil {
			t.Fatalf("Insert: %v", err)
		}
	}

	// Create vector store.
	db := chromem.NewDB()
	vs, err := NewVectorStore(db, "test-retrieval", retrievalEmbed)
	if err != nil {
		t.Fatalf("NewVectorStore: %v", err)
	}

	// Index the entries into the vector store.
	emb := &fixedEmbedder{dim: 8}
	for _, e := range entries {
		embeddings, eErr := emb.Embed(ctx, []string{entryText(e)})
		if eErr != nil {
			t.Fatalf("Embed: %v", eErr)
		}
		_ = vs.Upsert(ctx, e.ID, embeddings[0], entryMetadata(e))
	}

	// Create retriever and wire vector search.
	r := NewRetriever(s, 5)
	r.SetVectorSearch(vs, emb)

	// Retrieve should use vector search.
	result, err := r.Retrieve(ctx, "nginx")
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}

	if !result.Hit {
		t.Error("Hit: want true, got false")
	}
	if result.Method != "vector" {
		t.Errorf("Method: want vector, got %s", result.Method)
	}
	if len(result.Entries) == 0 {
		t.Error("Entries: want >0, got 0")
	}
}

// TestRetriever_Retrieve_VectorFallbackToKeyword verifies the retriever
// falls back to keyword when vector search returns no results.
func TestRetriever_Retrieve_VectorFallbackToKeyword(t *testing.T) {
	s := newTestSQLiteStore(t)
	ctx := context.Background()

	// Insert an entry.
	entry := MemoryEntry{
		ID: "fb1", SessionID: "s1", Tool: "shell",
		Command: "echo hello", Result: "hello",
	}
	if err := s.Insert(entry); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	// Create empty vector store (no vectors indexed).
	db := chromem.NewDB()
	vs, err := NewVectorStore(db, "test-fallback", retrievalEmbed)
	if err != nil {
		t.Fatalf("NewVectorStore: %v", err)
	}

	emb := &fixedEmbedder{dim: 8}
	r := NewRetriever(s, 5)
	r.SetVectorSearch(vs, emb)

	// Retrieve should fall back to keyword (vector store is empty).
	result, err := r.Retrieve(ctx, "echo")
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}

	if !result.Hit {
		t.Error("Hit: want true, got false")
	}
	if result.Method != "keyword" {
		t.Errorf("Method: want keyword, got %s", result.Method)
	}
	if len(result.Entries) != 1 {
		t.Errorf("Entries: want 1, got %d", len(result.Entries))
	}
}

// TestRetriever_Retrieve_MultiWordQuery matches if any word hits.
func TestRetriever_Retrieve_MultiWordQuery(t *testing.T) {
	ms := &mockStore{
		entries: []MemoryEntry{
			{ID: "1", Command: "systemctl restart nginx",
				Result: "ok"},
		},
	}
	r := NewRetriever(ms, 5)

	// Full sentence — should match on "nginx" word.
	result, err := r.Retrieve(context.Background(),
		"I asked for nginx restart but it failed",
	)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}

	if !result.Hit {
		t.Error("Hit: want true for multi-word query, got false")
	}
	if len(result.Entries) != 1 {
		t.Errorf("Entries: want 1, got %d", len(result.Entries))
	}
}

// TestRetriever_Retrieve_ShortWordsSkipped skips words under 2 chars.
func TestRetriever_Retrieve_ShortWordsSkipped(t *testing.T) {
	ms := &mockStore{
		entries: []MemoryEntry{
			{ID: "1", Command: "ls -la", Result: "files"},
		},
	}
	r := NewRetriever(ms, 5)

	// Query with only short words — should miss.
	result, err := r.Retrieve(context.Background(), "a b c")
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}

	if result.Hit {
		t.Error("Hit: want false for short-only query, got true")
	}
}

// TestRetriever_Retrieve_PunctuationStripped matches despite trailing
// punctuation like "?" or ".".
func TestRetriever_Retrieve_PunctuationStripped(t *testing.T) {
	ms := &mockStore{
		entries: []MemoryEntry{
			{ID: "1", Command: "uptime", Result: "up 5 days"},
		},
	}
	r := NewRetriever(ms, 5)

	// Query with trailing punctuation — should match "uptime".
	result, err := r.Retrieve(context.Background(),
		"What is my current uptime?",
	)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}

	if !result.Hit {
		t.Error("Hit: want true for punctuated query, got false")
	}
	if len(result.Entries) != 1 {
		t.Errorf("Entries: want 1, got %d", len(result.Entries))
	}
}

// TestRetriever_Retrieve_CaseInsensitive matches regardless of case.
func TestRetriever_Retrieve_CaseInsensitive(t *testing.T) {
	ms := &mockStore{
		entries: []MemoryEntry{
			{ID: "1", Command: "systemctl restart nginx",
				Result: "ok"},
		},
	}
	r := NewRetriever(ms, 5)

	result, err := r.Retrieve(context.Background(),
		"Show me the NGINX status",
	)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}

	if !result.Hit {
		t.Error("Hit: want true for case-insensitive match, got false")
	}
}
