package main

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/spf13/cobra"
	"shmorby/internal/config"
	"shmorby/internal/session"
)

var (
	sessionCmd = &cobra.Command{
		Use:   "session",
		Short: "List and manage persisted conversations",
	}
	sessionListCmd = &cobra.Command{
		Use:   "list",
		Short: "List root sessions for the current directory",
		RunE:  runSessionList,
	}
	sessionShowCmd = &cobra.Command{
		Use:   "show <id>",
		Short: "Show session metadata; --messages for the transcript",
		Args:  cobra.ExactArgs(1),
		RunE:  runSessionShow,
	}
	sessionRmCmd = &cobra.Command{
		Use:   "rm <id>",
		Short: "Archive a session; --force deletes rows",
		Args:  cobra.ExactArgs(1),
		RunE:  runSessionRm,
	}
	sessionPruneCmd = &cobra.Command{
		Use:   "prune",
		Short: "Apply retention_days/max_sessions cleanup",
		RunE:  runSessionPrune,
	}
)

var (
	sessionAllDirs    bool
	sessionLimit      int
	sessionMessages   bool
	sessionForce      bool
	sessionDBOverride string
)

func init() {
	sessionListCmd.Flags().BoolVar(
		&sessionAllDirs, "all-dirs", false,
		"include sessions from other launch directories")
	sessionListCmd.Flags().IntVar(
		&sessionLimit, "limit", 20, "max sessions to show")

	sessionShowCmd.Flags().BoolVar(
		&sessionMessages, "messages", false, "print the full transcript")

	sessionRmCmd.Flags().BoolVar(
		&sessionForce, "force", false, "delete rows instead of archiving")

	for _, c := range []*cobra.Command{
		sessionListCmd, sessionShowCmd, sessionRmCmd, sessionPruneCmd,
	} {
		c.Flags().StringVar(
			&sessionDBOverride, "db-path", "",
			"sessions db path (default: session.db_path from config)")
		sessionCmd.AddCommand(c)
	}

	rootCmd.AddCommand(sessionCmd)
}

// Opens the sessions database for a subcommand. Resolves the path
// from the layered config when it loads; falls back to the default
// path with a warning when the config is unusable (the same escape
// hatch audit subcommands have). --db-path overrides both.
func openSessionStoreCLI() (session.Store, error) {
	dbPath := session.DefaultDBPath()
	if sessionDBOverride == "" {
		cfg, err := config.Load(config.LoadOptions{})
		if err != nil {
			slog.Warn("session: config unavailable, using default db path",
				"err", err, "path", dbPath)
		} else if cfg.Session.DBPath != "" {
			dbPath = cfg.Session.DBPath
		}
	} else {
		dbPath = sessionDBOverride
	}
	return session.NewStore(dbPath)
}

// relativeTime renders t as a coarse "x ago" string for listings.
func relativeTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func runSessionList(cmd *cobra.Command, args []string) error {
	store, err := openSessionStoreCLI()
	if err != nil {
		return fmt.Errorf("open session db: %w", err)
	}
	defer store.Close()

	q := session.Query{Limit: sessionLimit}
	if !sessionAllDirs {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("resolve cwd: %w", err)
		}
		q.Directory = session.NormalizeDir(cwd)
	}

	metas, err := store.List(q)
	if err != nil {
		return fmt.Errorf("list sessions: %w", err)
	}
	if len(metas) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No sessions found.")
		return nil
	}

	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "%-38s %-60s %-12s %6s  %s\n",
		"ID", "TITLE", "UPDATED", "MSGS", "DIRECTORY")
	for _, m := range metas {
		fmt.Fprintf(w, "%-38s %-60s %-12s %6d  %s\n",
			m.ID, truncate(m.Title, 60), relativeTime(m.UpdatedAt),
			m.MessageCount, m.Directory)
	}
	return nil
}

func runSessionShow(cmd *cobra.Command, args []string) error {
	store, err := openSessionStoreCLI()
	if err != nil {
		return fmt.Errorf("open session db: %w", err)
	}
	defer store.Close()

	sess, m, err := store.Load(args[0])
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	archived := ""
	if !m.ArchivedAt.IsZero() {
		archived = m.ArchivedAt.Format(time.RFC3339)
	}
	fmt.Fprintf(w, "id:        %s\n", m.ID)
	fmt.Fprintf(w, "title:     %s\n", m.Title)
	fmt.Fprintf(w, "directory: %s\n", m.Directory)
	fmt.Fprintf(w, "agent:     %s\n", m.AgentMode)
	fmt.Fprintf(w, "provider:  %s\n", m.Provider)
	fmt.Fprintf(w, "model:     %s\n", m.Model)
	fmt.Fprintf(w, "created:   %s\n", m.CreatedAt.Format(time.RFC3339))
	fmt.Fprintf(w, "updated:   %s\n", m.UpdatedAt.Format(time.RFC3339))
	if archived != "" {
		fmt.Fprintf(w, "archived:  %s\n", archived)
	}
	msgs := sess.Messages()
	fmt.Fprintf(w, "messages:  %d\n", len(msgs))

	if !sessionMessages {
		return nil
	}
	for i, msg := range msgs {
		fmt.Fprintf(w, "\n[%d] %s", i+1, msg.Role)
		if msg.ToolName != "" {
			fmt.Fprintf(w, " (%s)", msg.ToolName)
		}
		fmt.Fprintf(w, ":\n%s\n", msg.Content)
		for _, tc := range msg.ToolCalls {
			fmt.Fprintf(w, "  tool_call %s args=%s\n", tc.Name, tc.Args)
		}
	}
	return nil
}

func runSessionRm(cmd *cobra.Command, args []string) error {
	store, err := openSessionStoreCLI()
	if err != nil {
		return fmt.Errorf("open session db: %w", err)
	}
	defer store.Close()

	id := args[0]
	if _, _, err := store.Load(id); err != nil {
		return err
	}

	if sessionForce {
		if err := store.Delete(id); err != nil {
			return fmt.Errorf("delete session: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Deleted session %s.\n", id)
		return nil
	}

	if err := store.Archive(id); err != nil {
		return fmt.Errorf("archive session: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(),
		"Archived session %s (use --force to delete rows).\n", id)
	return nil
}

func runSessionPrune(cmd *cobra.Command, args []string) error {
	// Retention values come from the layered config (AC9).
	cfg, err := config.Load(config.LoadOptions{})
	if err != nil {
		slog.Warn("prune: config unavailable, using defaults",
			"err", err)
		cfg = config.DefaultConfig()
	}

	store, err := openSessionStoreCLI()
	if err != nil {
		return fmt.Errorf("open session db: %w", err)
	}
	defer store.Close()

	olderThan := time.Duration(cfg.Session.RetentionDays) * 24 * time.Hour
	n, err := store.Prune(olderThan, cfg.Session.MaxSessions)
	if err != nil {
		return fmt.Errorf("prune sessions: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(),
		"Pruned %d session(s) (retention %dd, cap %d).\n",
		n, cfg.Session.RetentionDays, cfg.Session.MaxSessions)
	return nil
}

// truncate caps s to n runes for column output.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// openRootSession opens the session store per config and builds the
// root session: a fresh in-memory one when persistence is off, a
// new bound one by default, or a replayed one for
// --continue/--session. Returns a nil store when
// persistence is inactive.
func openRootSession(
	cmd *cobra.Command, cfg *config.Config,
) (session.Store, *session.Session, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, nil, fmt.Errorf("resolve cwd: %w", err)
	}
	// Canonical form for directory scoping (macOS /tmp, symlinked
	// mount points): stored and looked-up dirs must match exactly.
	cwd = session.NormalizeDir(cwd)

	resumeRequested := continueFlag || sessionFlag != ""

	// Persistence disabled: write nothing, resume flags error
	// cleanly (AC4).
	if !cfg.Session.Enabled {
		if resumeRequested {
			return nil, nil, fmt.Errorf(
				"session persistence is disabled " +
					"(session.enabled: false); " +
					"--continue/--session require it",
			)
		}
		return nil, session.New(), nil
	}

	store, err := session.NewStore(cfg.Session.DBPath)
	if err != nil {
		if resumeRequested {
			return nil, nil, fmt.Errorf("open session store: %w", err)
		}
		slog.Warn(
			"session store unavailable, "+
				"continuing without persistence",
			"err", err,
		)
		return nil, session.New(), nil
	}

	var (
		sess  *session.Session
		meta  session.Meta
		found bool
	)
	switch {
	case sessionFlag != "":
		// Unknown id fails loudly with a clear message (AC2).
		sess, meta, err = store.Load(sessionFlag)
		if err != nil {
			store.Close()
			return nil, nil, fmt.Errorf("resume session: %w", err)
		}
		found = true
	case continueFlag:
		meta, found = store.Latest(cwd)
		if !found {
			store.Close()
			return nil, nil, fmt.Errorf(
				"no previous session found for %s "+
					"(start a new session instead)",
				cwd,
			)
		}
		sess, meta, err = store.Load(meta.ID)
		if err != nil {
			store.Close()
			return nil, nil, fmt.Errorf("resume session: %w", err)
		}
	}

	if found {
		if err := applyResumeConfig(cmd, cfg, cwd, meta); err != nil {
			store.Close()
			return nil, nil, err
		}
	} else {
		sess = session.New()
	}

	// Directory after any resume chdir; provider/model/agent from
	// the (possibly reloaded) config.
	dir := cwd
	if d, derr := os.Getwd(); derr == nil {
		dir = session.NormalizeDir(d)
	}
	bind := session.Meta{
		ID:        sess.ID(),
		Title:     meta.Title,
		Directory: dir,
		AgentMode: cfg.Agent.Default,
		Provider:  cfg.Provider,
		Model:     cfg.Model,
		LastSeq:   meta.LastSeq,
	}
	sess.BindStore(store, bind)

	return store, sess, nil
}

// Handles the post-resume environment: chdir to the session's
// stored directory when it differs from the launch dir (AC3), and
// re-read config so cwd-local settings plus the session's start
// provider/model/agent (unless CLI flags override them) are in
// effect for the rest of startup.
func applyResumeConfig(
	cmd *cobra.Command,
	cfg *config.Config,
	launchDir string,
	meta session.Meta,
) error {
	if meta.Directory != "" {
		sessionDir := session.NormalizeDir(meta.Directory)
		if sessionDir != launchDir {
			st, err := os.Stat(sessionDir)
			if err != nil || !st.IsDir() {
				return fmt.Errorf(
					"session %s was started in %s, "+
						"which no longer exists; not resuming",
					meta.ID, meta.Directory,
				)
			}
			if err := os.Chdir(sessionDir); err != nil {
				return fmt.Errorf("chdir to session dir: %w", err)
			}
			// Visible notice: stdout for the REPL, TUI log pane
			// via slog, log file for postmortem.
			cmd.Printf("resumed session %q from %s\n",
				meta.Title, sessionDir)
			slog.Info("resumed session in stored directory",
				"session", meta.ID,
				"directory", sessionDir,
				"launch_dir", launchDir,
			)
		}
	}

	// Reload config: cwd-local ./shmorby.yaml is now the session
	// directory's, and the stored start settings flow through the
	// normal validation path.
	provider, model, agent := providerFlag, modelFlag, agentFlag
	if provider == "" && meta.Provider != "" {
		provider = meta.Provider
	}
	if model == "" && meta.Model != "" {
		model = meta.Model
	}
	if agent == "" && meta.AgentMode != "" {
		agent = meta.AgentMode
	}
	reloaded, err := config.Load(config.LoadOptions{
		ConfigFile: configFile, // absolute or empty: cwd-independent
		Provider:   provider,
		Model:      model,
		Agent:      agent,
	})
	if err != nil {
		return fmt.Errorf("reload config for resume: %w", err)
	}
	if reloaded.Session.DBPath != cfg.Session.DBPath {
		slog.Warn(
			"session.db_path changed after resume chdir; "+
				"keeping the store opened from the launch dir",
			"configured", reloaded.Session.DBPath,
		)
	}
	*cfg = reloaded

	return nil
}
