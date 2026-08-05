package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
)

//go:embed find.txt
var findDescription string

var findParams = json.RawMessage(`{
	"type": "object",
	"properties": {
		"path": {
			"type": "string",
			"description": "root directory to search from (default: current directory)"
		},
		"pattern": {
			"type": "string",
			"description": "glob pattern, e.g. *.go"
		},
		"type": {
			"type": "string",
			"description": "filter by type: file, dir, or empty (both)"
		},
		"max_depth": {
			"type": "integer",
			"description": "maximum directory depth (default: 100)"
		}
	},
	"required": ["pattern"]
}`)

type findArgs struct {
	Path     string `json:"path,omitempty"`
	Pattern  string `json:"pattern"`
	Type     string `json:"type,omitempty"`
	MaxDepth int    `json:"max_depth,omitempty"`
}

// Implements Tool for filesystem search using filepath.WalkDir.
// Avoids shell find hangs on stuck filesystems.
type FindTool struct {
	perm            string
	defaultMaxDepth int
}

// Creates a FindTool with the given permission level.
func NewFindTool(permLevel string) *FindTool {
	return &FindTool{
		perm:            permLevel,
		defaultMaxDepth: 100,
	}
}

// Returns the tool name.
func (f *FindTool) Name() string { return "find" }

// Returns the embedded LLM description.
func (f *FindTool) Description() string { return findDescription }

// Returns the JSON schema for find parameters.
func (f *FindTool) Parameters() json.RawMessage { return findParams }

// PermLevel returns the configured permission level.
func (f *FindTool) PermLevel() string { return f.perm }

// SetPerm updates the permission level at runtime.
func (f *FindTool) SetPerm(level string) { f.perm = level }

// Walk the filesystem with context cancellation support. Matches
// against glob pattern and filters by type/depth.
func (f *FindTool) Run(
	ctx context.Context, args json.RawMessage,
) (string, error) {
	var a findArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("invalid find args: %w", err)
	}
	if a.Pattern == "" {
		return "", fmt.Errorf(
			`find: missing required field "pattern"`,
		)
	}

	root := a.Path
	if root == "" {
		root = "."
	}
	maxDepth := a.MaxDepth
	if maxDepth <= 0 {
		maxDepth = f.defaultMaxDepth
	}

	var matches []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return filepath.SkipDir
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}

		if rel == "." {
			return nil
		}

		sepCount := strings.Count(rel, string(filepath.Separator))
		if sepCount >= maxDepth {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		if a.Type == "file" && d.IsDir() {
			return nil
		}
		if a.Type == "dir" && !d.IsDir() {
			return nil
		}

		matched, _ := filepath.Match(a.Pattern, d.Name())
		if matched {
			matches = append(matches, path)
			if len(matches) >= 500 {
				return filepath.SkipAll
			}
		}
		return nil
	})

	if err != nil && err != filepath.SkipAll && err != context.Canceled {
		return "", fmt.Errorf("find walk: %w", err)
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
