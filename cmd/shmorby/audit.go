package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"shmorby/internal/audit"
)

var auditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Query and manage the audit log",
}

var auditListCmd = &cobra.Command{
	Use:   "list",
	Short: "List audit entries with optional filters",
	RunE:  runAuditList,
}

var auditGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Show a single audit entry with output",
	Args:  cobra.ExactArgs(1),
	RunE:  runAuditGet,
}

var auditSessionCmd = &cobra.Command{
	Use:   "session <session_id>",
	Short: "Show all audit entries for a session (including subagents)",
	Args:  cobra.ExactArgs(1),
	RunE:  runAuditSession,
}

var auditExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export audit entries to file",
	RunE:  runAuditExport,
}

var auditVacuumCmd = &cobra.Command{
	Use:   "vacuum",
	Short: "Archive and remove old audit entries",
	RunE:  runAuditVacuum,
}

var auditStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show audit DB statistics",
	RunE:  runAuditStats,
}

var (
	auditSince       string
	auditTool        string
	auditLimit       int
	auditFormat      string
	auditBefore      string
	auditExitCode    int
	auditDecision    string
	auditExitCodeSet bool
)

func init() {
	auditListCmd.Flags().StringVar(&auditSince, "since", "", "show entries since duration (e.g. 24h, 7d)")
	auditListCmd.Flags().StringVar(&auditTool, "tool", "", "filter by tool name")
	auditListCmd.Flags().IntVar(&auditLimit, "limit", 20, "max entries to show")
	auditListCmd.Flags().IntVar(&auditExitCode, "exit-code", 0, "filter by exit code")
	auditListCmd.Flags().BoolVar(&auditExitCodeSet, "exit-code-set", false, "if set, filter by exit code")
	auditListCmd.Flags().StringVar(&auditDecision, "decision", "", "filter by permission decision (allow|deny)")

	auditExportCmd.Flags().StringVar(&auditSince, "since", "", "export entries since duration")
	auditExportCmd.Flags().StringVar(&auditFormat, "format", "json", "export format: json or csv")

	auditVacuumCmd.Flags().StringVar(&auditBefore, "before", "365d", "remove entries older than duration")

	auditCmd.AddCommand(auditListCmd)
	auditCmd.AddCommand(auditGetCmd)
	auditCmd.AddCommand(auditSessionCmd)
	auditCmd.AddCommand(auditExportCmd)
	auditCmd.AddCommand(auditVacuumCmd)
	auditCmd.AddCommand(auditStatsCmd)
}

func openAuditStore() (*audit.AuditStore, error) {
	dbPath := audit.DefaultDBPath()
	return audit.NewAuditStore(dbPath)
}

func parseDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}

	s = strings.TrimSpace(s)
	var mult time.Duration
	numStr := s

	switch {
	case strings.HasSuffix(s, "m"):
		mult = time.Minute
		numStr = strings.TrimSuffix(s, "m")
	case strings.HasSuffix(s, "h"):
		mult = time.Hour
		numStr = strings.TrimSuffix(s, "h")
	case strings.HasSuffix(s, "d"):
		mult = 24 * time.Hour
		numStr = strings.TrimSuffix(s, "d")
	case strings.HasSuffix(s, "w"):
		mult = 7 * 24 * time.Hour
		numStr = strings.TrimSuffix(s, "w")
	default:
		return 0, fmt.Errorf("invalid duration format %q (use e.g. 24h, 7d, 30m)", s)
	}

	n, err := strconv.Atoi(numStr)
	if err != nil {
		return 0, fmt.Errorf("parse duration: %w", err)
	}

	return time.Duration(n) * mult, nil
}

func runAuditList(cmd *cobra.Command, args []string) error {
	store, err := openAuditStore()
	if err != nil {
		return fmt.Errorf("open audit db: %w", err)
	}
	defer store.Close()

	// When --decision is set, list permission audit entries instead.
	if auditDecision != "" {
		f := audit.QueryFilter{
			Tool:     auditTool,
			Decision: auditDecision,
			Limit:    auditLimit,
		}
		if auditSince != "" {
			dur, err := parseDuration(auditSince)
			if err != nil {
				return err
			}
			f.Since = time.Now().Add(-dur)
		}

		perms, err := store.QueryPermissions(f)
		if err != nil {
			return fmt.Errorf("query permissions: %w", err)
		}
		if len(perms) == 0 {
			fmt.Fprintln(os.Stdout, "No permission audit entries found.")
			return nil
		}

		fmt.Fprintf(os.Stdout, "%-6s %-36s %-8s %-40s %-15s %-10s %s\n",
			"ID", "SESSION", "TOOL", "COMMAND", "RULE", "DECISION", "REASON")
		for _, p := range perms {
			cmd := p.Command
			if len(cmd) > 40 {
				cmd = cmd[:37] + "..."
			}
			fmt.Fprintf(os.Stdout, "%-6d %-36s %-8s %-40s %-15s %-10s %s\n",
				p.ID, p.SessionID, p.Tool, cmd,
				p.RulePattern, p.Decision, p.Reason)
		}
		return nil
	}

	f := audit.QueryFilter{
		Tool:  auditTool,
		Limit: auditLimit,
	}

	if auditExitCodeSet {
		f.ExitCode = &auditExitCode
	}

	if auditSince != "" {
		dur, err := parseDuration(auditSince)
		if err != nil {
			return err
		}
		f.Since = time.Now().Add(-dur)
	}

	entries, err := store.QueryEntries(f)
	if err != nil {
		return fmt.Errorf("query entries: %w", err)
	}

	if len(entries) == 0 {
		fmt.Fprintln(os.Stdout, "No audit entries found.")
		return nil
	}

	fmt.Fprintf(os.Stdout, "%-6s %-36s %-8s %-40s %-10s %-6s %s\n",
		"ID", "SESSION", "TOOL", "ARGS", "DURATION", "EXIT", "CAPTURED_AT")
	for _, e := range entries {
		args := e.Args
		if len(args) > 40 {
			args = args[:37] + "..."
		}
		exitCode := ""
		if e.ExitCode != nil {
			exitCode = strconv.Itoa(*e.ExitCode)
		}
		fmt.Fprintf(os.Stdout, "%-6d %-36s %-8s %-40s %-10d %-6s %s\n",
			e.ID, e.SessionID, e.Tool, args,
			e.DurationMs, exitCode, e.CapturedAt)
	}

	return nil
}

func runAuditGet(cmd *cobra.Command, args []string) error {
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid id: %w", err)
	}

	store, err := openAuditStore()
	if err != nil {
		return fmt.Errorf("open audit db: %w", err)
	}
	defer store.Close()

	entry, err := store.GetEntry(id)
	if err != nil {
		return fmt.Errorf("get entry: %w", err)
	}
	if entry == nil {
		return fmt.Errorf("entry %d not found", id)
	}

	fmt.Fprintf(os.Stdout, "ID:          %d\n", entry.ID)
	fmt.Fprintf(os.Stdout, "Session:     %s\n", entry.SessionID)
	fmt.Fprintf(os.Stdout, "Tool:        %s\n", entry.Tool)
	fmt.Fprintf(os.Stdout, "Args:        %s\n", entry.Args)
	fmt.Fprintf(os.Stdout, "Duration:    %dms\n", entry.DurationMs)
	if entry.ExitCode != nil {
		fmt.Fprintf(os.Stdout, "Exit Code:   %d\n", *entry.ExitCode)
	}
	if entry.Error != "" {
		fmt.Fprintf(os.Stdout, "Error:       %s\n", entry.Error)
	}
	fmt.Fprintf(os.Stdout, "Captured At: %s\n", entry.CapturedAt)

	output, err := store.GetOutput(id)
	if err != nil {
		return fmt.Errorf("get output: %w", err)
	}
	if output != nil {
		fmt.Fprintf(os.Stdout, "\n--- stdout (%d bytes) ---\n", output.StdoutSize)
		if output.Stdout != "" {
			fmt.Fprintln(os.Stdout, output.Stdout)
		}
		if output.Stderr != "" {
			fmt.Fprintf(os.Stdout, "\n--- stderr (%d bytes) ---\n", output.StderrSize)
			fmt.Fprintln(os.Stdout, output.Stderr)
		}
		if output.Checksum != "" {
			fmt.Fprintf(os.Stdout, "\nChecksum: %s\n", output.Checksum)
		}
	}

	return nil
}

func runAuditSession(cmd *cobra.Command, args []string) error {
	store, err := openAuditStore()
	if err != nil {
		return fmt.Errorf("open audit db: %w", err)
	}
	defer store.Close()

	sessionID := args[0]
	entries, err := store.GetSessionEntries(sessionID)
	if err != nil {
		return fmt.Errorf("get session entries: %w", err)
	}

	subs, err := store.GetSubagents(sessionID)
	if err != nil {
		return fmt.Errorf("get subagents: %w", err)
	}

	fmt.Fprintf(os.Stdout, "Session: %s\n", sessionID)

	if len(entries) == 0 && len(subs) == 0 {
		fmt.Fprintln(os.Stdout, "  No entries found.")
		return nil
	}

	for _, e := range entries {
		exitCode := 0
		if e.ExitCode != nil {
			exitCode = *e.ExitCode
		}
		fmt.Fprintf(os.Stdout, "  Entry %d: %s: %s [%dms, exit %d]\n",
			e.ID, e.Tool, e.Args, e.DurationMs, exitCode)
	}

	for _, sa := range subs {
		dur := ""
		if sa.DurationMs != nil {
			dur = fmt.Sprintf(", %dms", *sa.DurationMs)
		}
		fmt.Fprintf(os.Stdout, "  Subagent: %s [%s%s]\n",
			sa.ChildSessionID, sa.Status, dur)
	}

	return nil
}

func runAuditExport(cmd *cobra.Command, args []string) error {
	store, err := openAuditStore()
	if err != nil {
		return fmt.Errorf("open audit db: %w", err)
	}
	defer store.Close()

	f := audit.QueryFilter{}
	if auditSince != "" {
		dur, err := parseDuration(auditSince)
		if err != nil {
			return err
		}
		f.Since = time.Now().Add(-dur)
	}

	switch auditFormat {
	case "json":
		return store.ExportJSON(os.Stdout, f)
	case "csv":
		return store.ExportCSV(os.Stdout, f)
	default:
		return fmt.Errorf("unknown format %q (want json or csv)", auditFormat)
	}
}

func runAuditVacuum(cmd *cobra.Command, args []string) error {
	store, err := openAuditStore()
	if err != nil {
		return fmt.Errorf("open audit db: %w", err)
	}
	defer store.Close()

	dur, err := parseDuration(auditBefore)
	if err != nil {
		return err
	}

	cutoff := time.Now().Add(-dur)
	fmt.Fprintf(os.Stdout, "Vacuuming audit entries older than %s...\n",
		cutoff.Format("2006-01-02"))

	entries, perms, output, subs, err := store.Vacuum(dur)
	if err != nil {
		return fmt.Errorf("vacuum: %w", err)
	}

	fmt.Fprintf(os.Stdout, "  audit_entries: %d rows removed\n", entries)
	fmt.Fprintf(os.Stdout, "  audit_permissions: %d rows removed\n", perms)
	fmt.Fprintf(os.Stdout, "  audit_output: %d rows removed\n", output)
	fmt.Fprintf(os.Stdout, "  audit_subagents: %d rows removed\n", subs)

	return nil
}

func runAuditStats(cmd *cobra.Command, args []string) error {
	store, err := openAuditStore()
	if err != nil {
		return fmt.Errorf("open audit db: %w", err)
	}
	defer store.Close()

	stats, err := store.Stats()
	if err != nil {
		return fmt.Errorf("stats: %w", err)
	}

	fmt.Fprintf(os.Stdout, "Entries:     %d\n", stats.Entries)
	fmt.Fprintf(os.Stdout, "Permissions: %d\n", stats.Permissions)
	fmt.Fprintf(os.Stdout, "Output:      %d\n", stats.Output)
	fmt.Fprintf(os.Stdout, "Subagents:   %d\n", stats.Subagents)
	fmt.Fprintf(os.Stdout, "DB Size:     %s\n", formatBytes(stats.DBSize))

	return nil
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func init() {
	rootCmd.AddCommand(auditCmd)
}
