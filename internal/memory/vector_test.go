package memory

import (
	"context"
	"path/filepath"
	"testing"

	chromem "github.com/philippgille/chromem-go"
)

// fixedEmbedder returns deterministic embeddings for testing.
type fixedEmbedder struct {
	dim int
}

func (f *fixedEmbedder) Embed(
	_ context.Context, texts []string,
) ([][]float32, error) {
	results := make([][]float32, len(texts))
	for i, t := range texts {
		emb := make([]float32, f.dim)
		for j := range emb {
			emb[j] = float32(len(t)-j) * 0.001
		}
		results[i] = emb
	}

	return results, nil
}

func (f *fixedEmbedder) Dimension() int {
	return f.dim
}

func testEmbed(_ context.Context, text string) ([]float32, error) {
	emb := make([]float32, 8)
	for i := range emb {
		emb[i] = float32(len(text)-i) * 0.001
	}

	return emb, nil
}

func newTestVectorStore(t *testing.T) *VectorStore {
	t.Helper()

	db := chromem.NewDB()
	vs, err := NewVectorStore(db, "test", testEmbed)
	if err != nil {
		t.Fatalf("NewVectorStore: %v", err)
	}

	return vs
}

// TestNewVectorStore_CreatesCollection verifies collection is created.
func TestNewVectorStore_CreatesCollection(t *testing.T) {
	db := chromem.NewDB()
	vs, err := NewVectorStore(db, "memories", testEmbed)
	if err != nil {
		t.Fatalf("NewVectorStore: %v", err)
	}

	if vs.Count() != 0 {
		t.Errorf("want 0 docs, got %d", vs.Count())
	}
}

// TestVectorStore_Upsert_Search verifies add then search returns the doc.
func TestVectorStore_Upsert_Search(t *testing.T) {
	vs := newTestVectorStore(t)
	ctx := context.Background()

	emb := []float32{1, 0, 0, 0, 0, 0, 0, 0}
	err := vs.Upsert(ctx, "id-1", emb, map[string]string{
		"session_id": "s1",
	})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	if vs.Count() != 1 {
		t.Errorf("want 1 doc, got %d", vs.Count())
	}

	ids, err := vs.Search(ctx, emb, 10, nil)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(ids) != 1 || ids[0] != "id-1" {
		t.Errorf("want [id-1], got %v", ids)
	}
}

// TestVectorStore_Upsert_Overwrites verifies same ID overwrites.
func TestVectorStore_Upsert_Overwrites(t *testing.T) {
	vs := newTestVectorStore(t)
	ctx := context.Background()

	err := vs.Upsert(ctx, "id-1", []float32{1, 0, 0, 0, 0, 0, 0, 0}, nil)
	if err != nil {
		t.Fatalf("Upsert 1: %v", err)
	}

	err = vs.Upsert(ctx, "id-1", []float32{0, 1, 0, 0, 0, 0, 0, 0}, nil)
	if err != nil {
		t.Fatalf("Upsert 2: %v", err)
	}

	if vs.Count() != 1 {
		t.Errorf("want 1 doc after overwrite, got %d", vs.Count())
	}
}

// TestVectorStore_Delete removes vector by ID.
func TestVectorStore_Delete(t *testing.T) {
	vs := newTestVectorStore(t)
	ctx := context.Background()

	_ = vs.Upsert(ctx, "id-1", []float32{1, 0, 0, 0, 0, 0, 0, 0}, nil)
	_ = vs.Upsert(ctx, "id-2", []float32{0, 1, 0, 0, 0, 0, 0, 0}, nil)

	err := vs.Delete(ctx, "id-1")
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if vs.Count() != 1 {
		t.Errorf("want 1 doc after delete, got %d", vs.Count())
	}

	ids, err := vs.Search(ctx, []float32{1, 0, 0, 0, 0, 0, 0, 0}, 10, nil)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, id := range ids {
		if id == "id-1" {
			t.Error("deleted ID still in results")
		}
	}
}

// TestVectorStore_Search_Empty returns no results on empty collection.
func TestVectorStore_Search_Empty(t *testing.T) {
	vs := newTestVectorStore(t)
	ctx := context.Background()

	ids, err := vs.Search(ctx, []float32{1, 0, 0, 0, 0, 0, 0, 0}, 10, nil)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("want 0 results, got %d", len(ids))
	}
}

// TestVectorStore_Search_WithWhere filters by metadata.
func TestVectorStore_Search_WithWhere(t *testing.T) {
	vs := newTestVectorStore(t)
	ctx := context.Background()

	emb := []float32{1, 0, 0, 0, 0, 0, 0, 0}
	_ = vs.Upsert(ctx, "id-1", emb, map[string]string{"tool": "shell"})
	_ = vs.Upsert(ctx, "id-2", emb, map[string]string{"tool": "ssh"})

	ids, err := vs.Search(ctx, emb, 10, map[string]string{
		"tool": "shell",
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(ids) != 1 || ids[0] != "id-1" {
		t.Errorf("want [id-1], got %v", ids)
	}
}

// TestEntryText_BuildsCorrectString verifies text representation.
func TestEntryText_BuildsCorrectString(t *testing.T) {
	entry := MemoryEntry{
		Tool:    "shell",
		Command: "systemctl restart nginx",
		Summary: "restarted nginx",
	}

	got := entryText(entry)
	want := "shell: systemctl restart nginx restarted nginx"
	if got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

// TestEntryText_NoSummary omits trailing space.
func TestEntryText_NoSummary(t *testing.T) {
	entry := MemoryEntry{
		Tool:    "shell",
		Command: "ls",
	}

	got := entryText(entry)
	want := "shell: ls"
	if got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

// TestEntryMetadata_BuildsMap verifies metadata keys.
func TestEntryMetadata_BuildsMap(t *testing.T) {
	entry := MemoryEntry{
		SessionID: "s1",
		Tool:      "shell",
		Tags:      []string{"host:web01", "service:nginx"},
	}

	m := entryMetadata(entry)

	if m["session_id"] != "s1" {
		t.Errorf("session_id: want s1, got %s", m["session_id"])
	}
	if m["tool"] != "shell" {
		t.Errorf("tool: want shell, got %s", m["tool"])
	}
	if m["tags"] != "host:web01,service:nginx" {
		t.Errorf("tags: want host:web01,service:nginx, got %s", m["tags"])
	}
}

// TestEntryMetadata_EmptyTags omits tags key.
func TestEntryMetadata_EmptyTags(t *testing.T) {
	entry := MemoryEntry{
		SessionID: "s1",
	}

	m := entryMetadata(entry)

	if _, ok := m["tags"]; ok {
		t.Error("tags key should not be present when empty")
	}
}

// TestMigrateToVectors_ReindexesSQLite verifies migration from SQLite.
func TestMigrateToVectors_ReindexesSQLite(t *testing.T) {
	s := newTestSQLiteStore(t)
	ctx := context.Background()

	// Insert entries into SQLite.
	for i := 0; i < 5; i++ {
		entry := MemoryEntry{
			ID:        intToID(i),
			SessionID: "s1",
			Command:   "echo " + intToCommand(i),
			Tool:      "shell",
		}
		if err := s.Insert(entry); err != nil {
			t.Fatalf("Insert %d: %v", i, err)
		}
	}

	// Create vector store.
	db := chromem.NewDB()
	vs, err := NewVectorStore(db, "test-migrate", testEmbed)
	if err != nil {
		t.Fatalf("NewVectorStore: %v", err)
	}

	emb := &fixedEmbedder{dim: 8}

	// Run migration.
	err = s.migrateToVectors(ctx, vs, emb)
	if err != nil {
		t.Fatalf("migrateToVectors: %v", err)
	}

	if vs.Count() != 5 {
		t.Errorf("want 5 vectors after migration, got %d", vs.Count())
	}
}

// TestMigrateToVectors_SkipsWhenAlreadyIndexed verifies no-op if populated.
func TestMigrateToVectors_SkipsWhenAlreadyIndexed(t *testing.T) {
	s := newTestSQLiteStore(t)
	ctx := context.Background()

	entry := MemoryEntry{
		ID:        "a",
		SessionID: "s1",
		Command:   "echo hi",
	}
	_ = s.Insert(entry)

	db := chromem.NewDB()
	vs, err := NewVectorStore(db, "test-skip", testEmbed)
	if err != nil {
		t.Fatalf("NewVectorStore: %v", err)
	}

	// Pre-populate vector store.
	_ = vs.Upsert(ctx, "existing", []float32{1, 0, 0, 0, 0, 0, 0, 0}, nil)

	emb := &fixedEmbedder{dim: 8}
	err = s.migrateToVectors(ctx, vs, emb)
	if err != nil {
		t.Fatalf("migrateToVectors: %v", err)
	}

	// Should still have only 1 (the pre-existing one), not 2.
	if vs.Count() != 1 {
		t.Errorf("want 1 vector (skip migration), got %d", vs.Count())
	}
}

// TestMigrateToVectors_SkipsWhenNilVector verifies no-op with nil vector.
func TestMigrateToVectors_SkipsWhenNilVector(t *testing.T) {
	s := newTestSQLiteStore(t)
	emb := &fixedEmbedder{dim: 8}

	err := s.migrateToVectors(context.Background(), nil, emb)
	if err != nil {
		t.Fatalf("migrateToVectors: %v", err)
	}
}

// TestInsert_UpsertsVector verifies Insert upserts into vector store.
func TestInsert_UpsertsVector(t *testing.T) {
	dir := t.TempDir()
	cfg := defaultConfig()
	cfg.DBPath = filepath.Join(dir, "memory.db")

	emb := &fixedEmbedder{dim: 8}
	s, err := NewStore(cfg, emb)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer s.Close()

	ss := s.(*sqliteStore)
	vs := ss.VectorStore()
	if vs == nil {
		t.Fatal("vector store should be non-nil with embedder")
	}

	entry := MemoryEntry{
		ID:        "upsert-test",
		SessionID: "s1",
		Tool:      "shell",
		Command:   "echo test",
	}

	if err := s.Insert(entry); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	if vs.Count() != 1 {
		t.Errorf("want 1 vector after insert, got %d", vs.Count())
	}

	// Verify the vector can be found via search.
	embeddings, eErr := emb.Embed(
		context.Background(), []string{"shell: echo test"},
	)
	if eErr != nil {
		t.Fatalf("Embed: %v", eErr)
	}

	ids, sErr := vs.Search(
		context.Background(), embeddings[0], 10, nil,
	)
	if sErr != nil {
		t.Fatalf("Search: %v", sErr)
	}
	if len(ids) != 1 || ids[0] != "upsert-test" {
		t.Errorf("want [upsert-test], got %v", ids)
	}
}
