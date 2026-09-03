package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"shmorby/internal/config"
	"shmorby/internal/exec"
	"shmorby/internal/health"
	"shmorby/internal/util"
	"shmorby/internal/xdg"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Run self-diagnostics and report tool health",
	Long: `Run preflight checks for external and internal dependencies
and report degraded tooling.

Checks: local exec, sudo, ssh client, provider API reachability,
web fetch, aws CLI, memory (SQLite + Ollama embeddings), encrypted
ledger, audit trail (SQLite), config integrity, and XDG directory
structure. Degraded state is surfaced as a structured diagnostic —
use this to distinguish tooling faults from task failures.`,
	RunE: runDoctor,
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}

// Executes all health checks and prints a table.
// No behavior change to successful paths; thin wrapper only.
func runDoctor(cmd *cobra.Command, _ []string) error {
	ctx, cancel := context.WithTimeout(
		cmd.Context(), 30*time.Second,
	)
	defer cancel()

	cfg, err := config.Load(config.LoadOptions{})
	if err != nil {
		// Continue with defaults when config is unusable.
		fmt.Fprintf(os.Stderr,
			"warning: load config: %v (using defaults)\n", err)
		cfg = config.DefaultConfig()
	}

	// Use real executor and HTTP client for probes.
	exe := exec.OSExecutor{}
	client := &http.Client{Timeout: 5 * time.Second}

	info := health.ProviderInfo{
		Provider:           cfg.Provider,
		OllamaBaseURL:      cfg.Ollama.BaseURL,
		OpenAIBaseURL:      cfg.OpenAI.BaseURL,
		OpencodeZenBaseURL: cfg.OpencodeZen.BaseURL,
		MemoryProvider:     cfg.Memory.Embedding.Provider,
		MemoryDBPath:       util.ExpandPath(cfg.Memory.DBPath),
		AuditDBPath:        util.ExpandPath(cfg.Audit.DBPath),
		LedgerDir:          xdg.UserDataDir(),
		ConfigFilePath:     userConfigPath(),
	}
	results := health.RunAll(ctx, info, exe, client)

	// Print table header. Width accommodates longer check names
	// (e.g. "memory-sqlite", "audit-sqlite").
	fmt.Fprintf(os.Stdout,
		"%-14s %-10s %8s  %s\n",
		"CHECK", "STATUS", "DURATION", "DETAILS")
	fmt.Fprintln(os.Stdout,
		"------------------------------------------------------------")

	failed := 0
	for _, r := range results {
		status := r.Status
		if status != "ok" {
			failed++
		}

		fmt.Fprintf(os.Stdout,
			"%-14s %-10s %7dms  %s\n",
			r.Name, status, r.Duration.Milliseconds(), r.Details)
	}

	fmt.Fprintln(os.Stdout,
		"------------------------------------------------------------")

	if failed > 0 {
		fmt.Fprintf(os.Stdout,
			"%d check(s) degraded — see details above\n", failed)
		return fmt.Errorf("doctor: %d degraded", failed)
	}

	fmt.Fprintln(os.Stdout, "all checks ok")

	return nil
}

// userConfigPath returns the resolved user config file path for
// the current platform.
func userConfigPath() string {
	return filepath.Join(xdg.UserConfigDir(), "config.yaml")
}
