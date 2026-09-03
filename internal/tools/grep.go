package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var grepParams = json.RawMessage(`{
	"type": "object",
	"properties": {
		"pattern": {
			"type": "string",
			"description": "regex pattern to search for in file contents"
		},
		"path": {
			"type": "string",
			"description": "directory or file to search (default: current directory)"
		},
		"include": {
			"type": "string",
			"description": "glob pattern for files to include, e.g. \"*.go\""
		}
	},
	"required": ["pattern"]
}`)

type grepArgs struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path"`
	Include string `json:"include"`
}

// GrepTool searches file contents by regex pattern.
// When a ProjectRoot is configured, the default search root is the
// project directory and results are confined to it.
type GrepTool struct {
	perm string
	root *ProjectRoot
}

// NewGrepTool creates a GrepTool with the given permission level.
func NewGrepTool(permLevel string) *GrepTool {
	return &GrepTool{perm: permLevel}
}

// SetProjectRoot anchors grep searches to the given project root.
func (g *GrepTool) SetProjectRoot(r *ProjectRoot) { g.root = r }

// Name returns the tool name.
func (g *GrepTool) Name() string { return "grep" }

// Description returns the tool description.
func (g *GrepTool) Description() string {
	return `Search file contents using a regular expression pattern.
Returns matching lines with file path, line number, and content.
Use the include parameter to filter by file extension (e.g. "*.go").
Results are capped at 500 matches to prevent context flooding.`
}

// Parameters returns the JSON schema for grep parameters.
func (g *GrepTool) Parameters() json.RawMessage { return grepParams }

// PermLevel returns the configured permission level.
func (g *GrepTool) PermLevel() string { return g.perm }

// SetPerm updates the permission level at runtime.
func (g *GrepTool) SetPerm(level string) { g.perm = level }

// Run walks the directory tree and searches file contents by regex.
// The search root defaults to the project root when configured.
func (g *GrepTool) Run(
	ctx context.Context, args json.RawMessage,
) (string, error) {
	var a grepArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("invalid grep args: %w", err)
	}

	if a.Pattern == "" {
		return "", fmt.Errorf(`grep: missing required field "pattern"`)
	}

	re, err := regexp.Compile(a.Pattern)
	if err != nil {
		return "", fmt.Errorf("grep: invalid pattern %q: %w", a.Pattern, err)
	}

	// Resolve search root: explicit path > project root > ".".
	root := a.Path
	if root == "" && g.root != nil {
		root = g.root.Root
	} else if root == "" {
		root = "."
	} else if g.root != nil {
		// Confining an explicit path to the project root.
		var confErr error
		root, confErr = g.root.CheckPathConfinement(root)
		if confErr != nil {
			return "", fmt.Errorf("grep: %w", confErr)
		}
	}

	var matches []string

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if d.IsDir() {
			// Skip hidden directories and common non-project dirs.
			name := d.Name()
			if name == ".git" || name == "vendor" ||
				name == "node_modules" || name == ".idea" ||
				name == ".vscode" {
				return filepath.SkipDir
			}
			return nil
		}

		// Apply include filter if specified.
		if a.Include != "" {
			matched, _ := filepath.Match(a.Include, d.Name())
			if !matched {
				return nil
			}
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil // skip unreadable files
		}

		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			if re.MatchString(line) {
				matches = append(matches,
					fmt.Sprintf("%s:%d: %s", path, i+1, line),
				)
				if len(matches) >= 500 {
					return filepath.SkipAll
				}
			}
		}

		return nil
	})

	if walkErr != nil && walkErr != filepath.SkipAll && walkErr != context.Canceled {
		return "", fmt.Errorf("grep walk: %w", walkErr)
	}

	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	if len(matches) == 0 {
		return "no matches", nil
	}

	result := strings.Join(matches, "\n")

	return string(TruncateOutput([]byte(result))), nil
}
