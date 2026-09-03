package health

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"gopkg.in/yaml.v3"

	"shmorby/internal/ledger"
	"shmorby/internal/xdg"
)

// Executor runs external commands for health probing. Structurally
// identical to exec.Executor from internal/exec — any
// exec.OSExecutor satisfies this interface via Go's structural
// typing without an explicit import (avoids import cycle:
// exec → health → exec).
type Executor interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// HTTPClient performs HTTP requests for reachability checks.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Result holds a single preflight outcome.
type Result struct {
	Name     string
	Status   string
	Duration time.Duration
	Details  string
	Err      error
}

// All checks use a short per-check timeout.
var checkTimeout = 5 * time.Second

// ProviderInfo holds provider base URLs and internal system paths
// for reachability and integrity probing.
type ProviderInfo struct {
	Provider           string
	OllamaBaseURL      string
	OpenAIBaseURL      string
	OpencodeZenBaseURL string

	// MemoryProvider is the embedding provider for memory (e.g.
	// "ollama"). Separate from Provider — the chat provider may
	// differ from the embedding provider.
	MemoryProvider string

	// MemoryDBPath is the path to the SQLite memory database.
	MemoryDBPath string

	// AuditDBPath is the path to the SQLite audit database.
	AuditDBPath string

	// LedgerDir is the directory containing the encrypted ledger.
	LedgerDir string

	// ConfigFilePath is the path to the loaded config file (empty if
	// defaults only).
	ConfigFilePath string
}

// RunAll executes every preflight and returns results in stable order.
func RunAll(
	ctx context.Context,
	cfg ProviderInfo,
	exe Executor,
	client HTTPClient,
) []Result {
	if exe == nil {
		exe = execProbe{}
	}
	if client == nil {
		client = &http.Client{Timeout: checkTimeout}
	}

	checks := []func(context.Context) Result{
		func(c context.Context) Result {
			return CheckLocalExec(c, exe)
		},
		func(c context.Context) Result {
			return CheckSudo(c, exe)
		},
		func(c context.Context) Result {
			return CheckSSH(c, exe)
		},
		func(c context.Context) Result {
			return CheckProvider(c, cfg, client)
		},
		func(c context.Context) Result {
			return CheckWebFetch(c, client)
		},
		func(c context.Context) Result {
			return CheckAWS(c, exe)
		},
		func(c context.Context) Result {
			return CheckMemorySQLite(c, cfg)
		},
		func(c context.Context) Result {
			return CheckMemoryOllama(c, cfg, client)
		},
		func(c context.Context) Result {
			return CheckLedger(c, cfg)
		},
		func(c context.Context) Result {
			return CheckAuditSQLite(c, cfg)
		},
		func(c context.Context) Result {
			return CheckConfig(c, cfg)
		},
		func(c context.Context) Result {
			return CheckXDG(c)
		},
	}

	out := make([]Result, 0, len(checks))
	for _, fn := range checks {
		cCtx, cancel := context.WithTimeout(ctx, checkTimeout)
		r := fn(cCtx)
		cancel()

		out = append(out, r)
	}

	return out
}

// CheckLocalExec verifies basic local execution works.
func CheckLocalExec(ctx context.Context, exe Executor) Result {
	start := time.Now()

	out, err := exe.Run(ctx, "echo", "ok")
	if err != nil {
		// Retry via shell using the injected executor so
		// faked executors are exercised the same as prod.
		if out2, err2 := exe.Run(ctx, "sh", "-c", "echo ok"); err2 == nil {
			_ = out2

			return resultFrom(
				"local-exec", start, nil, "exec echo ok",
			)
		} else if _, ok := exe.(execProbe); ok {
			// Fallback for real executor on Windows where
			// echo is a shell builtin.
			if _, err3 := osexec.CommandContext(ctx,
				"sh", "-c", "echo ok").CombinedOutput(); err3 == nil {
				return resultFrom(
					"local-exec", start, nil, "exec echo ok",
				)
			} else if err2 != nil {
				err = err2
			} else {
				err = err3
			}
		}
		// Surface combined output when available.
		if len(out) > 0 {
			err = fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
		}
	}

	return resultFrom(
		"local-exec", start, err, "exec echo ok",
	)
}

// CheckSudo runs `sudo -n true` to detect missing or password-gated sudo.
func CheckSudo(ctx context.Context, exe Executor) Result {
	start := time.Now()

	out, err := exe.Run(ctx, "sudo", "-n", "true")
	if err != nil && len(out) > 0 {
		// Include combined output so Details shows
		// "a password is required" instead of bare exit status.
		err = fmt.Errorf("%w: %s", err,
			strings.TrimSpace(string(out)))
	}

	return resultFrom("sudo", start, err, "sudo -n true")
}

// CheckSSH verifies the ssh client exists and is callable.
func CheckSSH(ctx context.Context, exe Executor) Result {
	start := time.Now()

	if _, err := osexec.LookPath("ssh"); err != nil {
		return Result{
			Name:     "ssh",
			Status:   "missing",
			Duration: time.Since(start),
			Details:  "ssh binary not found in PATH",
			Err:      err,
		}
	}

	_, err := exe.Run(ctx, "ssh", "-V")
	if err != nil {
		// Missing wrapper (e.g. context) is degraded.
		if r := Classify(err); r == "missing" ||
			r == "timeout" || r == "unreachable" {
			return resultFrom("ssh", start, err, "ssh -V")
		}

		// Non-zero exit from ssh -V is not degraded if
		// the binary executed — still ok.
		if r := Classify(err); r == "error" {
			// Check if this is just an ExitError with
			// no degraded reason — treat as ok.
			return Result{
				Name:     "ssh",
				Status:   "ok",
				Duration: time.Since(start),
				Details:  "ssh client present",
			}
		}

		return resultFrom("ssh", start, err, "ssh -V")
	}

	return resultFrom("ssh", start, nil, "ssh client present")
}

// CheckProvider pings the configured LLM provider base URL.
func CheckProvider(
	ctx context.Context, cfg ProviderInfo, client HTTPClient,
) Result {
	start := time.Now()

	target, hint := providerURL(cfg)
	if target == "" {
		return Result{
			Name:     "provider",
			Status:   "ok",
			Duration: time.Since(start),
			Details:  "no provider URL (skipped)",
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return resultFrom("provider", start, err, hint)
	}

	resp, err := client.Do(req)
	if err != nil {
		return resultFrom("provider", start, err, hint)
	}
	defer resp.Body.Close()

	// Drain body to avoid leak.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

	// Any HTTP response means reachable — even 401/403 is not a
	// network failure (auth is separate from connectivity).
	if resp.StatusCode < 500 {
		return Result{
			Name:     "provider",
			Status:   "ok",
			Duration: time.Since(start),
			Details:  fmt.Sprintf("%s → %d", hint, resp.StatusCode),
		}
	}

	return Result{
		Name:     "provider",
		Status:   "degraded",
		Duration: time.Since(start),
		Details:  fmt.Sprintf("%s → %d", hint, resp.StatusCode),
		Err: fmt.Errorf(
			"provider returned %d", resp.StatusCode,
		),
	}
}

// CheckWebFetch probes external HTTP reachability.
func CheckWebFetch(ctx context.Context, client HTTPClient) Result {
	start := time.Now()

	u := "https://example.com/"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return resultFrom("webfetch", start, err, u)
	}

	resp, err := client.Do(req)
	if err != nil {
		return resultFrom("webfetch", start, err, u)
	}
	defer resp.Body.Close()

	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

	if resp.StatusCode >= 400 {
		return Result{
			Name:     "webfetch",
			Status:   "degraded",
			Duration: time.Since(start),
			Details:  fmt.Sprintf("GET %s → %d", u, resp.StatusCode),
			Err: fmt.Errorf(
				"webfetch returned %d", resp.StatusCode,
			),
		}
	}

	return Result{
		Name:     "webfetch",
		Status:   "ok",
		Duration: time.Since(start),
		Details:  fmt.Sprintf("GET %s → %d", u, resp.StatusCode),
	}
}

// CheckAWS verifies the aws CLI exists when enabled.
func CheckAWS(ctx context.Context, exe Executor) Result {
	start := time.Now()

	if _, err := osexec.LookPath("aws"); err != nil {
		return Result{
			Name:     "aws",
			Status:   "missing",
			Duration: time.Since(start),
			Details:  "aws CLI not found in PATH",
			Err:      err,
		}
	}

	_, err := exe.Run(ctx, "aws", "--version")

	return resultFrom("aws", start, err, "aws --version")
}

// CheckMemorySQLite verifies the SQLite memory database is present,
// not corrupted, and readable/writable. Reports DB size and location.
// Skips (ok) when no path is configured or the file does not yet exist.
func CheckMemorySQLite(ctx context.Context, cfg ProviderInfo) Result {
	start := time.Now()

	if cfg.MemoryDBPath == "" {
		return Result{
			Name:     "memory-sqlite",
			Status:   "ok",
			Duration: time.Since(start),
			Details:  "skipped (no memory DB path)",
		}
	}

	dbPath := cfg.MemoryDBPath
	if _, err := os.Stat(dbPath); err != nil {
		if os.IsNotExist(err) {
			return Result{
				Name:     "memory-sqlite",
				Status:   "ok",
				Duration: time.Since(start),
				Details:  "DB not yet created",
			}
		}
		return resultFrom("memory-sqlite", start, err,
			"stat "+dbPath)
	}

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return resultFrom("memory-sqlite", start, err, "open DB")
	}
	defer db.Close()

	// Verify connection.
	if err := db.PingContext(ctx); err != nil {
		return resultFrom("memory-sqlite", start, err, "ping DB")
	}

	// Integrity check.
	var integrity string
	if err := db.QueryRowContext(ctx,
		"PRAGMA integrity_check").Scan(&integrity); err != nil {
		return resultFrom("memory-sqlite", start, err,
			"integrity_check")
	}
	if integrity != "ok" {
		return Result{
			Name:     "memory-sqlite",
			Status:   "degraded",
			Duration: time.Since(start),
			Details:  fmt.Sprintf("integrity: %s", integrity),
			Err: fmt.Errorf(
				"integrity_check: %s", integrity),
		}
	}

	// Read/write probe: wrap in a transaction to pin all
	// operations to a single connection. Temp tables are
	// connection-scoped, and the pool may otherwise hand each
	// call to a different underlying connection.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return resultFrom("memory-sqlite", start, err,
			"begin probe tx")
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		"CREATE TEMPORARY TABLE IF NOT EXISTS _health_probe (v TEXT)"); err != nil {
		return resultFrom("memory-sqlite", start, err,
			"create probe table")
	}

	if _, err := tx.ExecContext(ctx,
		"INSERT INTO _health_probe (v) VALUES ('ok')"); err != nil {
		return resultFrom("memory-sqlite", start, err,
			"insert probe row")
	}

	// Verify the round-trip — value is read back to confirm
	// the temp table is queryable, not just present.
	var probe string
	if err := tx.QueryRowContext(ctx,
		"SELECT v FROM _health_probe").Scan(&probe); err != nil {
		return resultFrom("memory-sqlite", start, err,
			"read probe row")
	}
	if probe != "ok" {
		return Result{
			Name:     "memory-sqlite",
			Status:   "degraded",
			Duration: time.Since(start),
			Details:  fmt.Sprintf("probe round-trip mismatch: %q", probe),
			Err: fmt.Errorf(
				"probe value %q != ok", probe),
		}
	}

	_ = tx.Commit()

	// Report file size.
	info, err := os.Stat(dbPath)
	sizeStr := ""
	if err == nil {
		sizeStr = fmt.Sprintf(" (%s)", byteSize(info.Size()))
	}

	return Result{
		Name:     "memory-sqlite",
		Status:   "ok",
		Duration: time.Since(start),
		Details:  dbPath + sizeStr,
	}
}

// CheckMemoryOllama probes Ollama's /api/embed endpoint to verify the
// embedding endpoint works independently of the generic /api/tags check.
// Only runs when the memory embedding provider is "ollama"; skips (ok)
// otherwise.
func CheckMemoryOllama(
	ctx context.Context, cfg ProviderInfo, client HTTPClient,
) Result {
	start := time.Now()

	if cfg.MemoryProvider != "ollama" {
		return Result{
			Name:     "memory-ollama",
			Status:   "ok",
			Duration: time.Since(start),
			Details:  "skipped (memory provider is not ollama)",
		}
	}

	base := cfg.OllamaBaseURL
	if base == "" {
		base = "http://127.0.0.1:11434"
	}

	reqBody := map[string]any{
		"model": "nomic-embed-text",
		"input": []string{"health check"},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return resultFrom("memory-ollama", start, err,
			"marshal request")
	}

	embedURL := strings.TrimRight(base, "/") + "/api/embed"
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, embedURL, bytes.NewReader(body),
	)
	if err != nil {
		return resultFrom("memory-ollama", start, err,
			"create request")
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return resultFrom("memory-ollama", start, err, "POST /api/embed")
	}
	defer resp.Body.Close()

	// Check status before reading body — error responses may
	// have large or malformed bodies.
	if resp.StatusCode >= 500 {
		return Result{
			Name:     "memory-ollama",
			Status:   "degraded",
			Duration: time.Since(start),
			Details:  fmt.Sprintf("/api/embed → %d", resp.StatusCode),
			Err: fmt.Errorf(
				"/api/embed returned %d", resp.StatusCode),
		}
	}

	if resp.StatusCode >= 400 {
		// 400 can mean model not pulled — degraded but not
		// a connectivity failure.
		return Result{
			Name:     "memory-ollama",
			Status:   "degraded",
			Duration: time.Since(start),
			Details: fmt.Sprintf(
				"/api/embed → %d (model may not be pulled)",
				resp.StatusCode),
			Err: fmt.Errorf(
				"/api/embed returned %d", resp.StatusCode),
		}
	}

	// Only read body for successful responses.
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return resultFrom("memory-ollama", start, err, "read response")
	}

	// Verify the response contains a valid embedding vector.
	var parsed struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return resultFrom("memory-ollama", start, err,
			"parse embedding response")
	}
	if len(parsed.Embeddings) == 0 || len(parsed.Embeddings[0]) == 0 {
		return Result{
			Name:     "memory-ollama",
			Status:   "degraded",
			Duration: time.Since(start),
			Details:  "no embedding vector in response",
			Err: fmt.Errorf(
				"empty embedding response"),
		}
	}

	dim := len(parsed.Embeddings[0])

	return Result{
		Name:     "memory-ollama",
		Status:   "ok",
		Duration: time.Since(start),
		Details:  fmt.Sprintf("/api/embed → %d (%d-dim)", resp.StatusCode, dim),
	}
}

// CheckLedger verifies the encrypted ledger file exists, has safe
// permissions, and is decryptable. Reports entry count without
// exposing secrets. Skips (ok) when no ledger directory is configured.
func CheckLedger(ctx context.Context, cfg ProviderInfo) Result {
	start := time.Now()

	if cfg.LedgerDir == "" {
		return Result{
			Name:     "ledger",
			Status:   "ok",
			Duration: time.Since(start),
			Details:  "skipped (no ledger dir)",
		}
	}

	// The key file is created on first Open; its presence means the
	// ledger has been initialized even if no data file exists yet.
	keyFile := filepath.Join(cfg.LedgerDir, "ledger.key")
	_, keyErr := os.Stat(keyFile)

	dataFile := filepath.Join(cfg.LedgerDir, "ledger.json.age")
	info, err := os.Stat(dataFile)
	if err != nil {
		if os.IsNotExist(err) {
			// Data file absent — distinguish initialized (key
			// exists) from never-opened, surfacing stat
			// errors other than IsNotExist (e.g. ACL denied).
			if os.IsNotExist(keyErr) {
				return Result{
					Name:     "ledger",
					Status:   "ok",
					Duration: time.Since(start),
					Details:  "not yet created",
				}
			}
			if keyErr != nil {
				return resultFrom("ledger", start, keyErr,
					"stat ledger.key")
			}
			return Result{
				Name:     "ledger",
				Status:   "ok",
				Duration: time.Since(start),
				Details:  "no entries (key initialized)",
			}
		}
		return resultFrom("ledger", start, err, "stat ledger")
	}

	// Verify file permissions are not world-readable.
	mode := info.Mode().Perm()
	if mode&0o007 != 0 {
		return Result{
			Name:     "ledger",
			Status:   "degraded",
			Duration: time.Since(start),
			Details:  fmt.Sprintf("permissions %o (should be 0600)", mode),
			Err: fmt.Errorf(
				"ledger file permissions %o", mode),
		}
	}

	// Attempt to decrypt and read (without exposing secrets).
	ldg, err := ledger.OpenAt(cfg.LedgerDir)
	if err != nil {
		return resultFrom("ledger", start, err, "open ledger")
	}
	defer ldg.Close()

	sections := ldg.Sections()

	return Result{
		Name:     "ledger",
		Status:   "ok",
		Duration: time.Since(start),
		Details:  fmt.Sprintf("%s (%d entries)", dataFile, len(sections)),
	}
}

// CheckAuditSQLite verifies the SQLite audit database is present,
// not corrupted, and readable. Reports DB size and entry count.
// Skips (ok) when no path is configured or the file does not yet exist.
func CheckAuditSQLite(ctx context.Context, cfg ProviderInfo) Result {
	start := time.Now()

	if cfg.AuditDBPath == "" {
		return Result{
			Name:     "audit-sqlite",
			Status:   "ok",
			Duration: time.Since(start),
			Details:  "skipped (no audit DB path)",
		}
	}

	dbPath := cfg.AuditDBPath
	if _, err := os.Stat(dbPath); err != nil {
		if os.IsNotExist(err) {
			return Result{
				Name:     "audit-sqlite",
				Status:   "ok",
				Duration: time.Since(start),
				Details:  "DB not yet created",
			}
		}
		return resultFrom("audit-sqlite", start, err,
			"stat "+dbPath)
	}

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return resultFrom("audit-sqlite", start, err, "open DB")
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return resultFrom("audit-sqlite", start, err, "ping DB")
	}

	var integrity string
	if err := db.QueryRowContext(ctx,
		"PRAGMA integrity_check").Scan(&integrity); err != nil {
		return resultFrom("audit-sqlite", start, err,
			"integrity_check")
	}
	if integrity != "ok" {
		return Result{
			Name:     "audit-sqlite",
			Status:   "degraded",
			Duration: time.Since(start),
			Details:  fmt.Sprintf("integrity: %s", integrity),
			Err: fmt.Errorf(
				"integrity_check: %s", integrity),
		}
	}

	// Count entries (best-effort; table may not exist in
	// very old schemas, but the schema creates it).
	var entries int
	_ = db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM audit_entries").Scan(&entries)

	info, err := os.Stat(dbPath)
	sizeStr := ""
	if err == nil {
		sizeStr = fmt.Sprintf(" (%s)", byteSize(info.Size()))
	}

	return Result{
		Name:     "audit-sqlite",
		Status:   "ok",
		Duration: time.Since(start),
		Details:  fmt.Sprintf("%s (%d entries)%s", dbPath, entries, sizeStr),
	}
}

// CheckConfig verifies the config file parses without errors and
// required fields are present. Reports the config file path.
// Skips (ok) when no config file path is configured.
func CheckConfig(ctx context.Context, cfg ProviderInfo) Result {
	start := time.Now()

	if cfg.ConfigFilePath == "" {
		return Result{
			Name:     "config",
			Status:   "ok",
			Duration: time.Since(start),
			Details:  "skipped (no config file path)",
		}
	}

	path := cfg.ConfigFilePath
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Result{
				Name:     "config",
				Status:   "ok",
				Duration: time.Since(start),
				Details:  "no config file (using defaults)",
			}
		}
		return resultFrom("config", start, err, "read config")
	}

	// Parse YAML into a generic map for field checks.
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return resultFrom("config", start, err, "parse YAML")
	}

	// Check required fields. Provider is required for the
	// agent to function; model may be set via CLI flags so
	// it is tracked separately as informational-only.
	var degradedWarnings []string
	var infoWarnings []string

	if _, ok := doc["provider"]; !ok {
		degradedWarnings = append(degradedWarnings, "missing provider")
	}
	if _, ok := doc["model"]; !ok {
		infoWarnings = append(infoWarnings, "model not set (may use CLI flag)")
	}

	// Validate provider value if present.
	if p, ok := doc["provider"].(string); ok {
		switch p {
		case "ollama", "openai", "openrouter", "opencode_zen":
			// valid
		default:
			degradedWarnings = append(degradedWarnings,
				fmt.Sprintf("unknown provider %q", p))
		}
	}

	// Surface informational warnings in Details even on ok.
	details := path
	if len(infoWarnings) > 0 {
		details += " (" + strings.Join(infoWarnings, ", ") + ")"
	}

	if len(degradedWarnings) > 0 {
		return Result{
			Name:     "config",
			Status:   "degraded",
			Duration: time.Since(start),
			Details:  fmt.Sprintf("%s: %s", path, strings.Join(degradedWarnings, "; ")),
			Err: fmt.Errorf(
				"config issues: %s", strings.Join(degradedWarnings, "; ")),
		}
	}

	return Result{
		Name:     "config",
		Status:   "ok",
		Duration: time.Since(start),
		Details:  details,
	}
}

// CheckXDG verifies the XDG data and config directories exist and are
// writable. The cache dir is intentionally excluded — no code reads
// from, writes to, or creates it, so probing it is misleading.
func CheckXDG(ctx context.Context) Result {
	start := time.Now()

	dirs := []struct {
		name string
		dir  string
	}{
		{"data", xdg.UserDataDir()},
		{"config", xdg.UserConfigDir()},
	}

	var issues []string
	for _, d := range dirs {
		writable, err := isWritable(d.dir)
		if err != nil {
			issues = append(issues,
				fmt.Sprintf("%s: %s (%v)", d.name, d.dir, err))
		} else if !writable {
			issues = append(issues,
				fmt.Sprintf("%s: %s (not writable)", d.name, d.dir))
		}
	}

	if len(issues) > 0 {
		return Result{
			Name:     "xdg",
			Status:   "degraded",
			Duration: time.Since(start),
			Details:  strings.Join(issues, "; "),
			Err: fmt.Errorf(
				"XDG dirs: %s", strings.Join(issues, "; ")),
		}
	}

	return Result{
		Name:     "xdg",
		Status:   "ok",
		Duration: time.Since(start),
		Details:  "data/config all writable",
	}
}

// providerURL returns a probe URL and a display hint for the active provider.
func providerURL(cfg ProviderInfo) (string, string) {
	switch cfg.Provider {
	case "ollama":
		base := cfg.OllamaBaseURL
		if base == "" {
			base = "http://127.0.0.1:11434"
		}

		return strings.TrimRight(base, "/") + "/api/tags",
			"ollama " + base
	case "openai":
		base := cfg.OpenAIBaseURL
		if base == "" {
			base = "https://api.openai.com"
		}
		trimmed := strings.TrimRight(base, "/")
		// Avoid double /v1/models when base already ends with it.
		if strings.HasSuffix(trimmed, "/v1/models") {
			return trimmed, "openai " + base
		}
		if strings.HasSuffix(trimmed, "/v1") {
			return trimmed + "/models", "openai " + base
		}
		u, err := url.Parse(base)
		if err != nil {
			return base, "openai " + base
		}
		u.Path = strings.TrimRight(u.Path, "/") + "/v1/models"

		return u.String(), "openai " + base
	case "openrouter":
		return "https://openrouter.ai/api/v1/models",
			"openrouter https://openrouter.ai"
	case "opencode_zen":
		base := cfg.OpencodeZenBaseURL
		if base == "" {
			base = "https://opencode.ai/zen"
		}
		trimmed := strings.TrimRight(base, "/")
		if strings.HasSuffix(trimmed, "/v1/models") {
			return trimmed, "opencode_zen " + base
		}
		if strings.HasSuffix(trimmed, "/v1") {
			return trimmed + "/models",
				"opencode_zen " + base
		}

		return trimmed + "/v1/models",
			"opencode_zen " + base
	default:
		return "", ""
	}
}

// resultFrom builds a Result from an error and classified reason.
func resultFrom(
	name string, start time.Time, err error, okDetails string,
) Result {
	dur := time.Since(start)
	if err == nil {
		return Result{
			Name:     name,
			Status:   "ok",
			Duration: dur,
			Details:  okDetails,
		}
	}

	reason := Classify(err)
	status := "degraded"
	switch reason {
	case "missing":
		status = "missing"
	case "auth/creds":
		status = "degraded"
	case "timeout":
		status = "degraded"
	case "unreachable":
		status = "degraded"
	}

	details := err.Error()
	if len(details) > 200 {
		details = details[:200] + "…"
	}

	return Result{
		Name:     name,
		Status:   status,
		Duration: dur,
		Details:  details,
		Err:      err,
	}
}

// execProbe is the default executor using os/exec.
type execProbe struct{}

// Run executes a command with context and returns combined output.
func (execProbe) Run(
	ctx context.Context, name string, args ...string,
) ([]byte, error) {
	return osexec.CommandContext(ctx, name, args...).CombinedOutput()
}

// ── helpers ────────────────────────────────────────────────────

// byteSize formats a byte count as a human-readable string.
func byteSize(n int64) string {
	const (
		kiB = 1 << 10
		miB = 1 << 20
		giB = 1 << 30
	)
	switch {
	case n >= giB:
		return fmt.Sprintf("%.1fGiB", float64(n)/giB)
	case n >= miB:
		return fmt.Sprintf("%.1fMiB", float64(n)/miB)
	case n >= kiB:
		return fmt.Sprintf("%.1fKiB", float64(n)/kiB)
	default:
		return fmt.Sprintf("%dB", n)
	}
}

// isWritable checks if a directory exists and is writable by
// attempting to create and remove a temporary file inside it.
func isWritable(dir string) (bool, error) {
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, fmt.Errorf("does not exist")
		}
		return false, err
	}
	if !info.IsDir() {
		return false, fmt.Errorf("not a directory")
	}

	// Attempt to create a temp file to verify writability.
	f, err := os.CreateTemp(dir, ".healthprobe*")
	if err != nil {
		return false, fmt.Errorf("cannot create file: %w", err)
	}
	path := f.Name()
	f.Close()
	// Guarantee cleanup regardless of what follows.
	defer os.Remove(path)

	return true, nil
}
