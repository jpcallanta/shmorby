package health

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"shmorby/internal/ledger"
)

// mockExecutor for health checks.
type mockExe struct {
	out []byte
	err error
}

func (m *mockExe) Run(
	_ context.Context, _ string, _ ...string,
) ([]byte, error) {
	return m.out, m.err
}

// mockClient for HTTP with optional body.
type mockClient struct {
	code int
	body []byte
	err  error
}

func (m *mockClient) Do(_ *http.Request) (*http.Response, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.body != nil {
		return &http.Response{
			StatusCode: m.code,
			Body:       io.NopCloser(&bytesReader{data: m.body}),
		}, nil
	}
	return &http.Response{
		StatusCode: m.code,
		Body:       http.NoBody,
	}, nil
}

// bytesReader is a simple io.Reader from a byte slice.
type bytesReader struct {
	data []byte
	pos  int
}

func (r *bytesReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

// ── existing tests (preserved) ─────────────────────────────────

// TestProviderURL_DoubleV1 ensures no double /v1/models.
func TestProviderURL_DoubleV1(t *testing.T) {
	cases := []struct {
		info ProviderInfo
		want string
	}{
		{
			ProviderInfo{
				Provider:      "openai",
				OpenAIBaseURL: "https://api.example.com/v1",
			},
			"https://api.example.com/v1/models",
		},
		{
			ProviderInfo{
				Provider:      "openai",
				OpenAIBaseURL: "https://api.example.com/v1/models",
			},
			"https://api.example.com/v1/models",
		},
		{
			ProviderInfo{
				Provider:           "opencode_zen",
				OpencodeZenBaseURL: "https://opencode.ai/zen/v1",
			},
			"https://opencode.ai/zen/v1/models",
		},
	}

	for _, tc := range cases {
		got, _ := providerURL(tc.info)
		if got != tc.want {
			t.Errorf("providerURL(%+v) want %q, got %q",
				tc.info, tc.want, got)
		}
	}
}

// TestCheckSudo_IncludesOutput checks output is surfaced.
func TestCheckSudo_IncludesOutput(t *testing.T) {
	exe := &mockExe{
		out: []byte("sudo: a password is required"),
		err: fmt.Errorf("exit status 1"),
	}
	res := CheckSudo(context.Background(), exe)
	if res.Status != "degraded" {
		t.Errorf("want degraded, got %q", res.Status)
	}
	if !strings.Contains(res.Details, "password is required") {
		t.Errorf("want password hint in %q", res.Details)
	}
}

// TestCheckSSH_TimeoutDegraded ensures timeout is not ok.
func TestCheckSSH_TimeoutDegraded(t *testing.T) {
	exe := &mockExe{err: context.DeadlineExceeded}
	res := CheckSSH(context.Background(), exe)
	if res.Status == "ok" {
		t.Errorf("want not ok for timeout, got %q", res.Status)
	}
}

// TestCheckWebFetch_StatusDegraded checks >=400 degraded.
func TestCheckWebFetch_StatusDegraded(t *testing.T) {
	client := &mockClient{code: 500}
	res := CheckWebFetch(context.Background(), client)
	if res.Status != "degraded" {
		t.Errorf("want degraded for 500, got %q", res.Status)
	}

	client2 := &mockClient{code: 200}
	res2 := CheckWebFetch(context.Background(), client2)
	if res2.Status != "ok" {
		t.Errorf("want ok for 200, got %q", res2.Status)
	}
}

// TestRunAll_Order ensures stable order.
func TestRunAll_Order(t *testing.T) {
	info := ProviderInfo{Provider: "ollama"}
	exe := &mockExe{}
	client := &mockClient{code: 200}
	results := RunAll(context.Background(), info, exe, client)
	if len(results) != 12 {
		t.Fatalf("want 12 results, got %d", len(results))
	}
	want := []string{
		"local-exec", "sudo", "ssh", "provider", "webfetch",
		"aws", "memory-sqlite", "memory-ollama", "ledger",
		"audit-sqlite", "config", "xdg",
	}
	for i, n := range want {
		if results[i].Name != n {
			t.Errorf("index %d want %q, got %q", i, n, results[i].Name)
		}
	}
}

// ── memory-sqlite ──────────────────────────────────────────────

// TestCheckMemorySQLite_NoPath ensures skip when no path.
func TestCheckMemorySQLite_NoPath(t *testing.T) {
	res := CheckMemorySQLite(context.Background(), ProviderInfo{})
	if res.Status != "ok" {
		t.Errorf("want ok, got %q", res.Status)
	}
	if !strings.Contains(res.Details, "skipped") {
		t.Errorf("want skipped in %q", res.Details)
	}
}

// TestCheckMemorySQLite_NotExist ensures ok when DB not yet created.
func TestCheckMemorySQLite_NotExist(t *testing.T) {
	res := CheckMemorySQLite(context.Background(), ProviderInfo{
		MemoryDBPath: "/nonexistent/path/memory.db",
	})
	if res.Status != "ok" {
		t.Errorf("want ok, got %q", res.Status)
	}
	if !strings.Contains(res.Details, "not yet created") {
		t.Errorf("want not yet created in %q", res.Details)
	}
}

// TestCheckMemorySQLite_ValidDB ensures ok on a valid DB.
func TestCheckMemorySQLite_ValidDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "memory.db")

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE memory (
			id TEXT PRIMARY KEY,
			timestamp TEXT NOT NULL
		);
	`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	db.Close()

	res := CheckMemorySQLite(context.Background(), ProviderInfo{
		MemoryDBPath: dbPath,
	})
	if res.Status != "ok" {
		t.Errorf("want ok, got %q (%s)", res.Status, res.Details)
	}
	if !strings.Contains(res.Details, dbPath) {
		t.Errorf("want path in %q", res.Details)
	}
}

// TestCheckMemorySQLite_Corrupt ensures degraded on corruption.
func TestCheckMemorySQLite_Corrupt(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "memory.db")

	if err := os.WriteFile(dbPath, []byte("not a sqlite db"), 0o644); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}

	res := CheckMemorySQLite(context.Background(), ProviderInfo{
		MemoryDBPath: dbPath,
	})
	if res.Status != "degraded" {
		t.Errorf("want degraded, got %q (%s)", res.Status, res.Details)
	}
}

// ── memory-ollama ──────────────────────────────────────────────

// TestCheckMemoryOllama_SkipNonOllama ensures skip for non-ollama.
func TestCheckMemoryOllama_SkipNonOllama(t *testing.T) {
	client := &mockClient{code: 200}
	res := CheckMemoryOllama(context.Background(), ProviderInfo{
		MemoryProvider: "openai",
	}, client)
	if res.Status != "ok" {
		t.Errorf("want ok, got %q", res.Status)
	}
	if !strings.Contains(res.Details, "skipped") {
		t.Errorf("want skipped in %q", res.Details)
	}
}

// TestCheckMemoryOllama_EmbeddingSuccess ensures ok on valid response.
func TestCheckMemoryOllama_EmbeddingSuccess(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"embeddings": [][]float32{{0.1, 0.2, 0.3}},
	})
	client := &mockClient{code: 200, body: body}
	res := CheckMemoryOllama(context.Background(), ProviderInfo{
		MemoryProvider: "ollama",
	}, client)
	if res.Status != "ok" {
		t.Errorf("want ok, got %q (%s)", res.Status, res.Details)
	}
	if !strings.Contains(res.Details, "3-dim") {
		t.Errorf("want dim in %q", res.Details)
	}
}

// TestCheckMemoryOllama_ServerError ensures degraded on 500.
func TestCheckMemoryOllama_ServerError(t *testing.T) {
	client := &mockClient{code: 500}
	res := CheckMemoryOllama(context.Background(), ProviderInfo{
		MemoryProvider: "ollama",
	}, client)
	if res.Status != "degraded" {
		t.Errorf("want degraded, got %q", res.Status)
	}
}

// TestCheckMemoryOllama_EmptyEmbedding ensures degraded on empty.
func TestCheckMemoryOllama_EmptyEmbedding(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"embeddings": [][]float32{},
	})
	client := &mockClient{code: 200, body: body}
	res := CheckMemoryOllama(context.Background(), ProviderInfo{
		MemoryProvider: "ollama",
	}, client)
	if res.Status != "degraded" {
		t.Errorf("want degraded, got %q (%s)", res.Status, res.Details)
	}
}

// ── ledger ─────────────────────────────────────────────────────

// TestCheckLedger_NoDir ensures skip when no dir.
func TestCheckLedger_NoDir(t *testing.T) {
	res := CheckLedger(context.Background(), ProviderInfo{})
	if res.Status != "ok" {
		t.Errorf("want ok, got %q", res.Status)
	}
	if !strings.Contains(res.Details, "skipped") {
		t.Errorf("want skipped in %q", res.Details)
	}
}

// TestCheckLedger_NotYetCreated ensures ok when file absent.
func TestCheckLedger_NotYetCreated(t *testing.T) {
	dir := t.TempDir()
	res := CheckLedger(context.Background(), ProviderInfo{
		LedgerDir: dir,
	})
	if res.Status != "ok" {
		t.Errorf("want ok, got %q (%s)", res.Status, res.Details)
	}
	if !strings.Contains(res.Details, "not yet created") {
		t.Errorf("want not yet created in %q", res.Details)
	}
}

// TestCheckLedger_KeyInitialized ensures "no entries" message when
// the key file exists but no data file.
func TestCheckLedger_KeyInitialized(t *testing.T) {
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "ledger.key")
	if err := os.WriteFile(keyFile, []byte("dummy"), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	res := CheckLedger(context.Background(), ProviderInfo{
		LedgerDir: dir,
	})
	if res.Status != "ok" {
		t.Errorf("want ok, got %q (%s)", res.Status, res.Details)
	}
	if !strings.Contains(res.Details, "no entries") {
		t.Errorf("want no entries in %q", res.Details)
	}
}

// TestCheckLedger_WorldReadable ensures degraded on bad perms.
func TestCheckLedger_WorldReadable(t *testing.T) {
	dir := t.TempDir()
	dataFile := filepath.Join(dir, "ledger.json.age")
	if err := os.WriteFile(dataFile, []byte("data"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	res := CheckLedger(context.Background(), ProviderInfo{
		LedgerDir: dir,
	})
	if res.Status != "degraded" {
		t.Errorf("want degraded, got %q (%s)", res.Status, res.Details)
	}
}

// TestCheckLedger_ValidLedger ensures ok on a real ledger.
func TestCheckLedger_ValidLedger(t *testing.T) {
	dir := t.TempDir()
	l, err := ledger.OpenAt(dir)
	if err != nil {
		t.Fatalf("create ledger: %v", err)
	}
	// Write a section so the data file is created on disk.
	l.Set("test", json.RawMessage(`{"key":"value"}`))
	if err := l.Close(); err != nil {
		t.Fatalf("close ledger: %v", err)
	}

	res := CheckLedger(context.Background(), ProviderInfo{
		LedgerDir: dir,
	})
	if res.Status != "ok" {
		t.Errorf("want ok, got %q (%s)", res.Status, res.Details)
	}
	if !strings.Contains(res.Details, "1 entries") {
		t.Errorf("want 1 entries in %q", res.Details)
	}
}

// ── audit-sqlite ───────────────────────────────────────────────

// TestCheckAuditSQLite_NoPath ensures skip when no path.
func TestCheckAuditSQLite_NoPath(t *testing.T) {
	res := CheckAuditSQLite(context.Background(), ProviderInfo{})
	if res.Status != "ok" {
		t.Errorf("want ok, got %q", res.Status)
	}
}

// TestCheckAuditSQLite_NotExist ensures ok when DB not yet created.
func TestCheckAuditSQLite_NotExist(t *testing.T) {
	res := CheckAuditSQLite(context.Background(), ProviderInfo{
		AuditDBPath: "/nonexistent/audit.db",
	})
	if res.Status != "ok" {
		t.Errorf("want ok, got %q", res.Status)
	}
}

// TestCheckAuditSQLite_ValidDB ensures ok on a valid DB.
func TestCheckAuditSQLite_ValidDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "audit.db")

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE audit_entries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			tool TEXT NOT NULL,
			args TEXT NOT NULL,
			duration_ms INTEGER NOT NULL DEFAULT 0,
			exit_code INTEGER,
			error TEXT,
			output_hash TEXT,
			captured_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
	`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO audit_entries (session_id, tool, args)
		VALUES ('s1', 'shell', 'echo test'), ('s2', 'shell', 'ls');
	`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	db.Close()

	res := CheckAuditSQLite(context.Background(), ProviderInfo{
		AuditDBPath: dbPath,
	})
	if res.Status != "ok" {
		t.Errorf("want ok, got %q (%s)", res.Status, res.Details)
	}
	if !strings.Contains(res.Details, "2 entries") {
		t.Errorf("want 2 entries in %q", res.Details)
	}
}

// ── config ─────────────────────────────────────────────────────

// TestCheckConfig_NoPath ensures skip when no path.
func TestCheckConfig_NoPath(t *testing.T) {
	res := CheckConfig(context.Background(), ProviderInfo{})
	if res.Status != "ok" {
		t.Errorf("want ok, got %q", res.Status)
	}
	if !strings.Contains(res.Details, "skipped") {
		t.Errorf("want skipped in %q", res.Details)
	}
}

// TestCheckConfig_NotExist ensures ok when file absent.
func TestCheckConfig_NotExist(t *testing.T) {
	res := CheckConfig(context.Background(), ProviderInfo{
		ConfigFilePath: "/nonexistent/config.yaml",
	})
	if res.Status != "ok" {
		t.Errorf("want ok, got %q", res.Status)
	}
	if !strings.Contains(res.Details, "no config file") {
		t.Errorf("want no config file in %q", res.Details)
	}
}

// TestCheckConfig_Valid ensures ok on valid config.
func TestCheckConfig_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shmorby.yaml")
	content := "provider: ollama\nmodel: llama3.2\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	res := CheckConfig(context.Background(), ProviderInfo{
		ConfigFilePath: path,
	})
	if res.Status != "ok" {
		t.Errorf("want ok, got %q (%s)", res.Status, res.Details)
	}
	if !strings.Contains(res.Details, path) {
		t.Errorf("want path in %q", res.Details)
	}
}

// TestCheckConfig_MissingProvider ensures degraded without provider.
func TestCheckConfig_MissingProvider(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shmorby.yaml")
	content := "model: llama3.2\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	res := CheckConfig(context.Background(), ProviderInfo{
		ConfigFilePath: path,
	})
	if res.Status != "degraded" {
		t.Errorf("want degraded, got %q (%s)", res.Status, res.Details)
	}
	if !strings.Contains(res.Details, "missing provider") {
		t.Errorf("want missing provider in %q", res.Details)
	}
}

// TestCheckConfig_InvalidYAML ensures degraded on bad YAML.
func TestCheckConfig_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shmorby.yaml")
	content := "provider: ollama\n: invalid: yaml: [\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	res := CheckConfig(context.Background(), ProviderInfo{
		ConfigFilePath: path,
	})
	if res.Status != "degraded" {
		t.Errorf("want degraded, got %q (%s)", res.Status, res.Details)
	}
}

// TestCheckConfig_UnknownProvider ensures degraded on unknown provider.
func TestCheckConfig_UnknownProvider(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shmorby.yaml")
	content := "provider: notreal\nmodel: m\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	res := CheckConfig(context.Background(), ProviderInfo{
		ConfigFilePath: path,
	})
	if res.Status != "degraded" {
		t.Errorf("want degraded, got %q (%s)", res.Status, res.Details)
	}
	if !strings.Contains(res.Details, "unknown provider") {
		t.Errorf("want unknown provider in %q", res.Details)
	}
}

// ── xdg ────────────────────────────────────────────────────────

// TestCheckXDG_AllWritable ensures ok when all dirs writable.
func TestCheckXDG_AllWritable(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(base, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, "config"))

	for _, dir := range []string{
		filepath.Join(base, "data", "shmorby"),
		filepath.Join(base, "config", "shmorby"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}

	res := CheckXDG(context.Background())
	if res.Status != "ok" {
		t.Errorf("want ok, got %q (%s)", res.Status, res.Details)
	}
}

// TestCheckXDG_MissingDir ensures degraded when a dir is missing.
func TestCheckXDG_MissingDir(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(base, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, "config"))

	if err := os.MkdirAll(
		filepath.Join(base, "data", "shmorby"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	res := CheckXDG(context.Background())
	if res.Status != "degraded" {
		t.Errorf("want degraded, got %q (%s)", res.Status, res.Details)
	}
}

// TestCheckConfig_InformationalModelWarning ensures model-missing
// is not degraded but appears in details.
func TestCheckConfig_InformationalModelWarning(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shmorby.yaml")
	content := "provider: ollama\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	res := CheckConfig(context.Background(), ProviderInfo{
		ConfigFilePath: path,
	})
	if res.Status != "ok" {
		t.Errorf("want ok, got %q (%s)", res.Status, res.Details)
	}
	if !strings.Contains(res.Details, "model not set") {
		t.Errorf("want model info in %q", res.Details)
	}
}
