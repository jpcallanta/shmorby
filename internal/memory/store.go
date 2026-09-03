package memory

import (
	"context"
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

	"shmorby/internal/util"
	"shmorby/internal/xuuid"
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
    tags TEXT,
    accessed INTEGER NOT NULL DEFAULT 0
);
`

const migrationAddAccessed = `
    ALTER TABLE memory ADD COLUMN accessed INTEGER NOT NULL DEFAULT 0;
`

// Creates or opens the store at the configured path.
func NewStore(cfg Config, embedder Embedder) (Store, error) {
	dbPath := util.ExpandPath(cfg.DBPath)

	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create memory dir: %w", err)
	}

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}

	// Run migration to add accessed column if missing (ignore error
	// if column already exists).
	_, _ = db.Exec(migrationAddAccessed)

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

func (s *sqliteStore) Insert(entry MemoryEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.config.MaxEntries > 0 {
		if err := s.evictIfNeededLocked(); err != nil {
			return fmt.Errorf("evict: %w", err)
		}
	}

	if entry.ID == "" {
		id, err := xuuid.New()
		if err != nil {
			return fmt.Errorf("generate id: %w", err)
		}

		entry.ID = id
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

	// Prefer to evict entries older than 7 days first, then low-access
	// entries, then oldest as fallback.
	sevenDaysAgo := time.Now().Add(-7 * 24 * time.Hour).UTC().Format(
		time.RFC3339,
	)
	result, err := s.db.Exec(`
		DELETE FROM memory WHERE rowid IN (
			SELECT rowid FROM memory
			ORDER BY
				CASE WHEN timestamp < ? THEN 0 ELSE 1 END,
				accessed ASC,
				timestamp ASC
			LIMIT ?
		)
	`, sevenDaysAgo, excess)
	if err != nil {
		return fmt.Errorf("evict entries: %w", err)
	}

	n, _ := result.RowsAffected()
	slog.Debug("evicted memory entries", "count", n)

	return nil
}

// IncrementAccess bumps the access counter for an entry. Best-effort:
// errors are swallowed.
func (s *sqliteStore) IncrementAccess(id string) {
	_, _ = s.db.Exec(
		"UPDATE memory SET accessed = accessed + 1 WHERE id = ?", id,
	)
}

func (s *sqliteStore) Get(id string) (MemoryEntry, error) {
	row := s.db.QueryRow(`
		SELECT id, timestamp, session_id, tool, command, args, result,
		       exit_code, summary, tags
		FROM memory WHERE id = ?
	`, id)

	entry, err := s.scanEntry(row)
	if err == nil {
		s.IncrementAccess(id)
	}

	return entry, err
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
		s.warnDimensionMismatch(ctx, vs, emb)
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

// warnDimensionMismatch logs a warning when the persisted vector
// collection holds embeddings of a different dimension than the
// configured embedder produces. Mixed-dimension collections make
// chromem's dot product fail on length mismatch, and the retriever
// swallows those search errors, silently degrading memory lookup.
func (s *sqliteStore) warnDimensionMismatch(
	ctx context.Context, vs *VectorStore, emb Embedder,
) {
	dim := emb.Dimension()
	if dim <= 0 {
		return
	}

	entries, err := s.List(1, 0)
	if err != nil || len(entries) == 0 {
		return
	}

	doc, err := vs.Collection.GetByID(ctx, entries[0].ID)
	if err != nil || len(doc.Embedding) == 0 {
		return
	}

	if len(doc.Embedding) != dim {
		slog.Warn(
			"existing vector index dimension differs from configured "+
				"embedding dimension; clear the vector index to rebuild",
			"stored_dimension", len(doc.Embedding),
			"configured_dimension", dim,
		)
	}
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

// Scans a JSON-style quoted string array "[a,b,c]" without
// encoding/json. The scan is quote-aware: commas, spaces, and other
// separators inside quotes belong to the tag, and \" and \\ are
// unescaped so tags containing quotes round-trip instead of being
// silently split or corrupted.
func parseTagsJSON(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" || s == "[]" || s == "null" {
		return nil
	}

	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")

	var (
		tags    []string
		cur     strings.Builder
		inQuote bool
		escaped bool
	)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case escaped:
			cur.WriteByte(c)
			escaped = false

		case inQuote && c == '\\':
			escaped = true

		case c == '"':
			if inQuote {
				tags = append(tags, cur.String())
				cur.Reset()
			}
			inQuote = !inQuote

		case inQuote:
			cur.WriteByte(c)
		}
		// Outside quotes only separators remain: commas and
		// whitespace, which the switch simply skips.
	}
	// Unterminated quote means corrupt data; emit what we have
	// rather than dropping it silently.
	if inQuote {
		tags = append(tags, cur.String())
	}

	// Historical behaviour dropped empty tags; keep that.
	var result []string
	for _, t := range tags {
		if t != "" {
			result = append(result, t)
		}
	}

	return result
}

// Escapes the characters that are special inside quoted tags.
var tagEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`)

// Joins strings with quotes: a,b,c → "a","b","c". Backslashes and
// quotes inside a part are escaped so the result re-parses to the
// original values.
func quoteJoin(parts []string, sep string) string {
	var b strings.Builder
	for i, p := range parts {
		if i > 0 {
			b.WriteString(sep)
		}
		b.WriteString("\"")
		b.WriteString(tagEscaper.Replace(p))
		b.WriteString("\"")
	}

	return b.String()
}
