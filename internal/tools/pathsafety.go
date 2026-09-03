package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// blockedDirNames are directory names that write tools must never
// modify. Read tools (grep, find) enforce this during walk; write
// tools must check before creating or overwriting files.
var blockedDirNames = map[string]bool{
	".git":         true,
	"vendor":       true,
	"node_modules": true,
	".idea":        true,
	".vscode":      true,
}

// CheckPathSafety verifies a file path does not target a blocked
// directory. Symlinks are resolved (best effort) and the resolved
// path is re-checked, so a link outside a blocked dir that points
// into one is still rejected — the no-root fallback used to accept
// /tmp/evil -> ~/.git/config outright. Returns nil when the
// path is safe, or a descriptive error when it targets .git/,
// vendor/, node_modules/, etc.
func CheckPathSafety(path string) error {
	clean := filepath.Clean(path)
	if err := checkBlockedSegments(clean); err != nil {
		return err
	}

	resolved := resolveSymlinksBestEffort(clean)
	if resolved != clean {
		if err := checkBlockedSegments(resolved); err != nil {
			return err
		}
	}

	return nil
}

// checkBlockedSegments walks every segment of the given path and
// rejects any that names a blocked directory.
func checkBlockedSegments(clean string) error {
	// The fixed-point check (Dir(seg) == seg) terminates at any
	// filesystem root: "/" on Unix, "C:\\" on Windows, and UNC roots.
	seg := clean
	for seg != "." && seg != "" {
		base := filepath.Base(seg)
		if blockedDirNames[base] {
			return fmt.Errorf(
				"refusing to modify path inside %q: %s",
				base, clean,
			)
		}
		parent := filepath.Dir(seg)
		if parent == seg {
			break
		}
		seg = parent
	}

	return nil
}

// resolveSymlinksBestEffort returns path with symlinks evaluated.
// Non-existent targets (e.g. a file about to be written) are
// resolved as far as their nearest existing ancestor, so a symlinked
// parent directory is still followed. Unresolvable paths are
// returned unchanged; callers rely on this only for extra checks,
// never to make a bad path pass.
func resolveSymlinksBestEffort(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}

	dir := path
	for {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			if realDir, rErr := filepath.EvalSymlinks(dir); rErr == nil {
				rel, relErr := filepath.Rel(dir, path)
				if relErr == nil {
					return filepath.Join(realDir, rel)
				}
			}
			return path
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return path
		}
		dir = parent
	}
}

// ProjectRoot anchors file tools to a project directory, matching
// opencode's behavior: the launch CWD becomes the working project
// and all file operations are confined to it by default.
type ProjectRoot struct {
	// Root is the absolute path to the project directory.
	Root string

	// AllowedPatterns are glob patterns permitted even when they
	// fall outside the project root.
	AllowedPatterns []string

	// BlockedPatterns are extra glob patterns always rejected.
	// Built-in blocked directory names (.git/, vendor/, etc.) are
	// always enforced via CheckPathSafety regardless of this list.
	BlockedPatterns []string
}

// NewProjectRoot creates a ProjectRoot from config values. The workdir
// parameter is the raw config value ("." means CWD). Returns the
// resolved absolute root path and a ProjectRoot, or an error if the
// root cannot be resolved.
func NewProjectRoot(
	workdir string,
	allowed, blocked []string,
) (*ProjectRoot, error) {
	root := workdir
	if root == "" || root == "." {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("resolve project root: %w", err)
		}
		root = cwd
	}

	// Expand ~/ or ~ prefix.
	if strings.HasPrefix(root, "~/") || root == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve project root tilde: %w", err)
		}
		if root == "~" {
			root = home
		} else {
			root = filepath.Join(home, root[2:])
		}
	}

	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve project root abs: %w", err)
	}

	return &ProjectRoot{
		Root:            abs,
		AllowedPatterns: allowed,
		BlockedPatterns: blocked,
	}, nil
}

// CheckPathConfinement resolves and validates a candidate path. It:
//  1. Resolves relative paths against the project root
//  2. Cleans the resolved path
//  3. Evaluates symlinks to prevent ../ + symlink escapes
//  4. Checks blocked directory names via CheckPathSafety (authoritative)
//  5. Checks AllowedPatterns (explicit opt-in to operate outside root)
//  6. Enforces project-root confinement (paths resolving outside the
//     root are rejected unless matched by AllowedPatterns)
//  7. Checks against additional BlockedPatterns via filepath.Match
//
// Paths outside the project root are blocked by default; the only way
// to operate on them is to list an explicit AllowedPattern. This makes
// the confinement model an enforced boundary rather than a prompt-only
// suggestion. Blocked directory names (.git/, node_modules/,
// etc.) are always hard-rejected via the segment walk in CheckPathSafety.
//
// Returns the final resolved absolute path or an error.
func (pr *ProjectRoot) CheckPathConfinement(
	path string,
) (string, error) {
	if pr == nil {
		// No project root configured; fall back to basic safety.
		return path, CheckPathSafety(path)
	}

	// Step 1: resolve relative paths against the project root.
	resolved := path
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(pr.Root, resolved)
	}

	// Step 2: clean to normalize ../ segments.
	resolved = filepath.Clean(resolved)

	// Step 3: evaluate symlinks to prevent symlink escapes.
	evaluated := resolveSymlinksBestEffort(resolved)

	// Step 4: authoritative blocked-directory check via segment walk.
	// This catches .git/, vendor/, node_modules/, .idea/, .vscode/
	// at ANY depth — filepath.Match cannot do this (Go treats ** as *).
	if err := CheckPathSafety(evaluated); err != nil {
		return "", err
	}

	// Compute the path relative to the (canonical) root so glob patterns
	// may be written relative to it. A Rel error means the two cannot be
	// compared at all (e.g. different Windows drive letters).
	realRoot := filepath.Clean(pr.Root)
	if r, rErr := filepath.EvalSymlinks(realRoot); rErr == nil {
		realRoot = r
	}
	relToRoot, relErr := filepath.Rel(realRoot, evaluated)
	if relToRoot == "." {
		relToRoot = ""
	}

	// matchesAny reports whether pattern matches the path in any of the
	// forms a user might reasonably configure: relative-to-root, the
	// symlink-evaluated absolute path, or the cleaned pre-evaluation
	// path (covers literal configs that traverse a symlinked dir such
	// as macOS /var -> /private/var).
	matchesAny := func(pattern string) bool {
		for _, candidate := range []string{relToRoot, evaluated, resolved} {
			if matched, _ := filepath.Match(pattern, candidate); matched {
				return true
			}
		}
		return false
	}

	// Step 5: check allowed patterns first — they override additional
	// BlockedPatterns and the root-confinement boundary; the built-in
	// blocked directory names (.git/, etc.) are always enforced via
	// CheckPathSafety above.
	for _, pattern := range pr.AllowedPatterns {
		if matchesAny(pattern) {
			return evaluated, nil
		}
	}

	// Step 6: enforce project-root confinement. A path that resolves
	// outside the root and was not explicitly allowlisted above is
	// rejected. The root is symlink-canonicalized first so a
	// project checked out under a linked directory (e.g. /home -> /mnt)
	// still matches its evaluated children. The escape test uses the
	// root-relative path rather than a string prefix so a filesystem
	// root ("/" or a drive letter) does not false-reject every path,
	// and non-comparable paths (failed Rel) count as outside.
	escaped := relErr != nil || relToRoot == ".." ||
		strings.HasPrefix(relToRoot, ".."+string(filepath.Separator))
	if escaped {
		return "", fmt.Errorf(
			"path escapes project root: %s", path,
		)
	}

	// Step 7: check additional blocked glob patterns.
	for _, pattern := range pr.BlockedPatterns {
		if matchesAny(pattern) {
			return "", fmt.Errorf(
				"path matches blocked pattern %q: %s",
				pattern, path,
			)
		}
	}

	return evaluated, nil
}
