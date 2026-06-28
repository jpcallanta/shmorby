package memory

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
	chromem "github.com/philippgille/chromem-go"
)

// Store defines the memory storage interface.
type Store interface {

	// CRUD

	Insert(entry MemoryEntry) error
	Get(id string) (MemoryEntry, error)
	Delete(id string) error
	List(limit, offset int) ([]MemoryEntry, error)

	// Stats

	Count() (int, error)
	Close() error

	// Config introspection

	AutoCaptureEnabled() bool
	TagRules() []TagRule
}

type sqliteStore struct {
	db       *sql.DB
	config   Config
	mu       sync.Mutex
	vector   *VectorStore
	embedder Embedder
}

const schema = `
CREATE TABLE IF NOT EXISTS memory (
    id TEXT PRIMARY KEY,
    timestamp TEXT NOT NULL,
    session_id TEXT NOT NULL,
    tool TEXT,
    command TEXT,
    args TEXT,
    result TEXT,
    exit_code INTEGER,
    summary TEXT,
    tags TEXT
);
`

// Creates or opens the store at the configured path.
func NewStore(cfg Config, embedder Embedder) (Store, error) {
	dbPath := expandPath(cfg.DBPath)

	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create memory dir: %w", err)
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}

	s := &sqliteStore{
		db:       db,
		config:   cfg,
		embedder: embedder,
	}

	// Initialize vector store when embedder is available.
	if embedder != nil {
		vs, vErr := newVectorStoreForMemory(dbPath, embedder)
		if vErr != nil {
			slog.Warn("vector store unavailable",
				"err", vErr)
		} else {
			s.vector = vs
		}
	}

	return s, nil
}

// Creates an in-process chromem-go vector store alongside the SQLite DB.
func newVectorStoreForMemory(
	dbPath string, emb Embedder,
) (*VectorStore, error) {
	dir := filepath.Dir(dbPath)
	vDir := filepath.Join(dir, "vectors")

	if err := os.MkdirAll(vDir, 0755); err != nil {
		return nil, fmt.Errorf("create vector dir: %w", err)
	}

	db, err := chromem.NewPersistentDB(vDir, false)
	if err != nil {
		return nil, fmt.Errorf("open chromem db: %w", err)
	}

	embedFunc := func(ctx context.Context, text string) ([]float32, error) {
		results, err := emb.Embed(ctx, []string{text})
		if err != nil {
			return nil, fmt.Errorf("embed text: %w", err)
		}
		if len(results) == 0 {
			return nil, fmt.Errorf("no embedding returned")
		}

		return results[0], nil
	}

	vs, err := NewVectorStore(db, "memory", embedFunc)
	if err != nil {
		return nil, fmt.Errorf("create vector store: %w", err)
	}

	return vs, nil
}

// Expands a leading ~/ to the user's home directory.
func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}

	return path
}

// Generates a UUID v4 string using crypto/rand.
func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80

	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func (s *sqliteStore) Insert(entry MemoryEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.config.MaxEntries > 0 {
		if err := s.evictIfNeededLocked(); err != nil {
			return fmt.Errorf("evict: %w", err)
		}
	}

	if entry.ID == "" {
		entry.ID = newUUID()
	}
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}

	entry.Result = truncateResult(entry.Result)

	tagsJSON := "[]"
	if len(entry.Tags) > 0 {
		tagsJSON = "[" + quoteJoin(entry.Tags, ",") + "]"
	}

	_, err := s.db.Exec(`
		INSERT INTO memory (id, timestamp, session_id, tool, command, args, result, exit_code, summary, tags)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, entry.ID, entry.Timestamp.UTC().Format(time.RFC3339),
		entry.SessionID, entry.Tool, entry.Command,
		entry.Args, entry.Result, entry.ExitCode,
		entry.Summary, tagsJSON)

	if err != nil {
		return fmt.Errorf("insert: %w", err)
	}

	// Upsert vector embedding if available.
	if s.vector != nil && s.embedder != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		text := entryText(entry)
		embeddings, embErr := s.embedder.Embed(ctx, []string{text})
		if embErr == nil && len(embeddings) > 0 {
			_ = s.vector.Upsert(ctx, entry.ID,
				embeddings[0], entryMetadata(entry))
		}
	}

	return nil
}

// Evicts oldest entries if count exceeds MaxEntries. Must be called
// with mu held.
func (s *sqliteStore) evictIfNeededLocked() error {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM memory").Scan(&count)
	if err != nil {
		return fmt.Errorf("count entries: %w", err)
	}

	if count < s.config.MaxEntries {
		return nil
	}

	excess := count - s.config.MaxEntries + 1
	if excess <= 0 {
		excess = 1
	}

	result, err := s.db.Exec(`
		DELETE FROM memory WHERE rowid IN (
			SELECT rowid FROM memory ORDER BY timestamp ASC LIMIT ?
		)
	`, excess)
	if err != nil {
		return fmt.Errorf("evict entries: %w", err)
	}

	n, _ := result.RowsAffected()
	slog.Debug("evicted memory entries", "count", n)

	return nil
}

func (s *sqliteStore) Get(id string) (MemoryEntry, error) {
	row := s.db.QueryRow(`
		SELECT id, timestamp, session_id, tool, command, args, result,
		       exit_code, summary, tags
		FROM memory WHERE id = ?
	`, id)

	return s.scanEntry(row)
}

func (s *sqliteStore) Delete(id string) error {
	result, err := s.db.Exec("DELETE FROM memory WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete: %w", err)
	}

	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("memory entry %q not found", id)
	}

	// Remove vector if present.
	if s.vector != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = s.vector.Delete(ctx, id)
	}

	return nil
}

func (s *sqliteStore) List(limit, offset int) ([]MemoryEntry, error) {
	rows, err := s.db.Query(`
		SELECT id, timestamp, session_id, tool, command, args, result,
		       exit_code, summary, tags
		FROM memory ORDER BY timestamp DESC LIMIT ? OFFSET ?
	`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return s.scanEntries(rows)
}

func (s *sqliteStore) Count() (int, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM memory").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count entries: %w", err)
	}
	return count, nil
}

func (s *sqliteStore) Close() error {
	return s.db.Close()
}

// VectorStore returns the vector store, or nil if unavailable.
func (s *sqliteStore) VectorStore() *VectorStore {
	return s.vector
}

// Embedder returns the embedder, or nil if unavailable.
func (s *sqliteStore) Embedder() Embedder {
	return s.embedder
}

// StoreVectorSearch extracts vector store and embedder from a Store.
// Returns nil values when vector search is not configured.
func StoreVectorSearch(s Store) (*VectorStore, Embedder) {
	ss, ok := s.(*sqliteStore)
	if !ok {
		return nil, nil
	}

	return ss.VectorStore(), ss.Embedder()
}

// StoreMigrateVectors runs migration on the store if vector search
// is configured. No-op otherwise.
func StoreMigrateVectors(
	ctx context.Context, s Store,
) error {
	ss, ok := s.(*sqliteStore)
	if !ok {
		return nil
	}

	vs := ss.VectorStore()
	emb := ss.Embedder()
	if vs == nil || emb == nil {
		return nil
	}

	return ss.MigrateToVectors(ctx, vs, emb)
}

func (s *sqliteStore) AutoCaptureEnabled() bool {
	return s.config.AutoCapture
}

func (s *sqliteStore) SetAutoCapture(enabled bool) {
	s.config.AutoCapture = enabled
}

func (s *sqliteStore) TagRules() []TagRule {
	return s.config.Tags
}

// MigrateToVectors re-indexes existing SQLite entries into the
// vector store when the collection is empty.
func (s *sqliteStore) MigrateToVectors(ctx context.Context, vs *VectorStore, emb Embedder) error {
	return s.migrateToVectors(ctx, vs, emb)
}

func (s *sqliteStore) migrateToVectors(
	ctx context.Context, vs *VectorStore, emb Embedder,
) error {
	if vs == nil || emb == nil {
		return nil
	}

	count := vs.Count()
	if count > 0 {
		return nil
	}

	entries, err := s.List(100000, 0)
	if err != nil {
		return fmt.Errorf("list for migration: %w", err)
	}

	for i := 0; i < len(entries); i += 32 {
		end := i + 32
		if end > len(entries) {
			end = len(entries)
		}

		batch := entries[i:end]
		texts := make([]string, len(batch))
		for j, e := range batch {
			texts[j] = entryText(e)
		}

		embeddings, embErr := emb.Embed(ctx, texts)
		if embErr != nil {
			slog.Warn("migration embed failed", "err", embErr)
			continue
		}

		for j, e := range batch {
			if j < len(embeddings) {
				_ = vs.Upsert(ctx, e.ID,
					embeddings[j], entryMetadata(e))
			}
		}
	}

	slog.Info("migrated memory entries to vector index",
		"count", len(entries))

	return nil
}

func (s *sqliteStore) scanEntry(
	row *sql.Row,
) (MemoryEntry, error) {
	var (
		entry     MemoryEntry
		timestamp string
		tagsJSON  string
	)

	err := row.Scan(&entry.ID, &timestamp, &entry.SessionID,
		&entry.Tool, &entry.Command, &entry.Args,
		&entry.Result, &entry.ExitCode, &entry.Summary,
		&tagsJSON)
	if err != nil {
		return entry, fmt.Errorf("scan: %w", err)
	}

	entry.Timestamp, _ = time.Parse(time.RFC3339, timestamp)
	entry.Tags = parseTagsJSON(tagsJSON)

	return entry, nil
}

func (s *sqliteStore) scanEntries(
	rows *sql.Rows,
) ([]MemoryEntry, error) {
	var entries []MemoryEntry

	for rows.Next() {
		var (
			entry     MemoryEntry
			timestamp string
			tagsJSON  string
		)

		err := rows.Scan(&entry.ID, &timestamp, &entry.SessionID,
			&entry.Tool, &entry.Command, &entry.Args,
			&entry.Result, &entry.ExitCode, &entry.Summary,
			&tagsJSON)
		if err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}

		entry.Timestamp, _ = time.Parse(time.RFC3339, timestamp)
		entry.Tags = parseTagsJSON(tagsJSON)
		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}

	return entries, nil
}

// Parses a simple JSON string array "[a,b,c]" without encoding/json.
func parseTagsJSON(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" || s == "[]" || s == "null" {
		return nil
	}

	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")

	if s == "" {
		return nil
	}

	var tags []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		part = strings.Trim(part, "\"")
		if part != "" {
			tags = append(tags, part)
		}
	}

	return tags
}

// Joins strings with quotes: a,b,c → "a","b","c".
func quoteJoin(parts []string, sep string) string {
	var b strings.Builder
	for i, p := range parts {
		if i > 0 {
			b.WriteString(sep)
		}
		b.WriteString("\"")
		b.WriteString(p)
		b.WriteString("\"")
	}

	return b.String()
}
