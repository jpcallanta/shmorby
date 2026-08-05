package scope

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"shmorby/internal/config"
	"shmorby/internal/fileread"
	"shmorby/internal/xdg"
)

// Flags holds CLI flag values that affect scope loading.
type Flags struct {
	// ScopeFile is the --scope-file flag value (error if set but missing).
	ScopeFile string
}

// LoadResult holds scope content and metadata for the /scope command.
type LoadResult struct {
	Content      string
	PrimaryPath  string
	Instructions []string
	TotalBytes   int
}

// Load resolves scope content with precedence:
// 1. --scope-file (error if path set but missing)
// 2. Walk cwd → parents for SCOPE.md
// 3. xdg.UserConfigDir()/SCOPE.md if exists
// 4. Append files from config.scope.instructions (literal paths)
//
// Returns merged scope content with source metadata.
func Load(cfg config.Config, flags Flags) (LoadResult, error) {
	// 1: --scope-file flag takes highest precedence.
	if flags.ScopeFile != "" {
		content, err := fileread.ReadFileLimited(flags.ScopeFile, 0)
		if err != nil {
			return LoadResult{}, fmt.Errorf(
				"load --scope-file: %w",
				err,
			)
		}

		merged, resolved, err := mergeInstructions(string(content), cfg.Scope.Instructions)
		if err != nil {
			return LoadResult{}, fmt.Errorf("merge --scope-file instructions: %w", err)
		}

		return LoadResult{
			Content:      merged,
			PrimaryPath:  flags.ScopeFile,
			Instructions: resolved,
			TotalBytes:   len(merged),
		}, nil
	}

	// 2: Walk cwd → parents for SCOPE.md.
	scopeContent, found, primaryPath := findScopeFile()
	if found {
		merged, resolved, err := mergeInstructions(scopeContent, cfg.Scope.Instructions)
		if err != nil {
			return LoadResult{}, fmt.Errorf("merge walked SCOPE.md instructions: %w", err)
		}

		return LoadResult{
			Content:      merged,
			PrimaryPath:  primaryPath,
			Instructions: resolved,
			TotalBytes:   len(merged),
		}, nil
	}

	// 3: ~/.config/shmorby/SCOPE.md if exists.
	scopeContent, found, primaryPath = findUserScopeFile()
	if found {
		merged, resolved, err := mergeInstructions(scopeContent, cfg.Scope.Instructions)
		if err != nil {
			return LoadResult{}, fmt.Errorf("merge user SCOPE.md instructions: %w", err)
		}

		return LoadResult{
			Content:      merged,
			PrimaryPath:  primaryPath,
			Instructions: resolved,
			TotalBytes:   len(merged),
		}, nil
	}

	// 4: Return empty content with instruction paths from config.
	merged, resolved, err := mergeInstructions("", cfg.Scope.Instructions)
	if err != nil {
		return LoadResult{}, fmt.Errorf("merge instructions: %w", err)
	}

	return LoadResult{
		Content:      merged,
		Instructions: resolved,
		TotalBytes:   len(merged),
	}, nil
}

// mergeInstructions reads each path in paths and appends its content to
// primary. Returns merged content and the list of paths actually resolved.
// Paths are literal; glob is optional stretch.
// Missing instruction files are silently skipped (optional stretch).
// Oversized files are logged at WARN level and skipped.
func mergeInstructions(primary string, paths []string) (string, []string, error) {
	var merged strings.Builder
	merged.WriteString(primary)

	resolved := make([]string, 0, len(paths))

	for _, path := range paths {
		// Skip empty paths.
		if path == "" {
			continue
		}
		// Use size-limited read to prevent OOM from oversized files (issue #46).
		content, err := fileread.ReadFileLimited(path, 0)
		if err != nil {
			if os.IsNotExist(err) {
				// Skip missing instruction files (optional stretch).
				continue
			}
			// Distinguish oversized files from other errors (MINOR-3).
			slog.Warn("instruction file skipped",
				"path", path, "err", err)
			continue
		}
		resolved = append(resolved, path)
		merged.WriteString("\n---\n")
		merged.Write(content)
	}

	return merged.String(), resolved, nil
}

// Searches for SCOPE.md from cwd up to root.
// Logs a warning if a SCOPE.md is found but exceeds the size limit.
func findScopeFile() (string, bool, string) {
	wd, err := os.Getwd()
	if err != nil {
		return "", false, ""
	}

	root := xdg.RootPrefix()
	// Walk from cwd up to root.
	for dir := wd; dir != root; dir = filepath.Dir(dir) {
		path := filepath.Join(dir, "SCOPE.md")
		// Use size-limited read to prevent OOM from oversized files (issue #46).
		content, err := fileread.ReadFileLimited(path, 0)
		if err == nil {
			return string(content), true, path
		}
		if !os.IsNotExist(err) {
			// File exists but is oversized or unreadable — warn instead
			// of silently falling through (MINOR-3).
			slog.Warn("SCOPE.md skipped",
				"path", path, "err", err)
		}
		// Stop at root.
		if dir == filepath.Dir(dir) {
			break
		}
	}

	// Check root directory.
	path := filepath.Join(root, "SCOPE.md")
	content, err := fileread.ReadFileLimited(path, 0)
	if err == nil {
		return string(content), true, path
	}
	if !os.IsNotExist(err) {
		slog.Warn("SCOPE.md skipped",
			"path", path, "err", err)
	}

	return "", false, ""
}

// Checks user-level scope via xdg.UserConfigDir.
// Logs a warning if the file exceeds the size limit.
func findUserScopeFile() (string, bool, string) {
	path := filepath.Join(xdg.UserConfigDir(), "SCOPE.md")
	// Use size-limited read to prevent OOM from oversized files (issue #46).
	content, err := fileread.ReadFileLimited(path, 0)
	if err == nil {
		return string(content), true, path
	}
	if !os.IsNotExist(err) {
		slog.Warn("user SCOPE.md skipped",
			"path", path, "err", err)
	}
	return "", false, ""
}
