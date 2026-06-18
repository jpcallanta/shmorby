package navigation

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RefItem is a single autocomplete entry.
type RefItem struct {
	Label string
	Value string
	Kind  string
}

// ReferenceSource is a source of @-reference items.
type ReferenceSource struct {
	Alias string
	Path  string
	Items []RefItem
}

// ReferenceEngine provides @-reference autocomplete.
type ReferenceEngine struct {
	sources []ReferenceSource
}

// NewReferenceEngine creates an empty reference engine.
func NewReferenceEngine() *ReferenceEngine {
	return &ReferenceEngine{}
}

// AddSource registers a reference source.
func (e *ReferenceEngine) AddSource(src ReferenceSource) {
	e.sources = append(e.sources, src)
}

// Complete returns fuzzy-matched ref items for the given query (without @).
func (e *ReferenceEngine) Complete(query string) []RefItem {
	if query == "" {
		var all []RefItem
		for _, src := range e.sources {
			all = append(all, RefItem{
				Label: "@" + src.Alias,
				Value: src.Alias,
				Kind:  "source",
			})
			for _, item := range src.Items {
				all = append(all, item)
			}
		}
		return all
	}
	q := strings.ToLower(query)
	var matches []RefItem
	for _, src := range e.sources {
		if fuzzyMatch(src.Alias, q) {
			matches = append(matches, RefItem{
				Label: "@" + src.Alias,
				Value: src.Alias,
				Kind:  "source",
			})
		}
		for _, item := range src.Items {
			if fuzzyMatch(item.Label, q) || fuzzyMatch(item.Value, q) {
				matches = append(matches, item)
			}
		}
	}
	return matches
}

// Resolve expands a @-reference to its resolved value.
// Source aliases read and return the source path's file content.
// File items read and return their content.
// Host/service items return their value as-is.
func (e *ReferenceEngine) Resolve(alias string) (string, string, error) {
	for _, src := range e.sources {
		if src.Alias == alias {
			if src.Path != "" {
				data, err := os.ReadFile(src.Path)
				if err == nil {
					return string(data), "file", nil
				}
			}
			return src.Path, "file", nil
		}
		for _, item := range src.Items {
			if item.Value == alias || item.Label == alias {
				if item.Kind == "file" || item.Kind == "" {
					data, err := os.ReadFile(item.Value)
					if err != nil {
						return "", "file", err
					}
					return string(data), "file", nil
				}
				return item.Value, item.Kind, nil
			}
		}
	}
	return "", "", nil
}

// Sources returns the registered sources.
func (e *ReferenceEngine) Sources() []ReferenceSource {
	out := make([]ReferenceSource, len(e.sources))
	copy(out, e.sources)
	return out
}

// AddFileReferences adds file paths matching a glob pattern under
// a source alias. The pattern is passed to filepath.Glob.
func (e *ReferenceEngine) AddFileReferences(
	alias, pattern string,
) error {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("glob %q: %w", pattern, err)
	}
	var items []RefItem
	for _, path := range matches {
		items = append(items, RefItem{
			Label: filepath.Base(path),
			Value: path,
			Kind:  "file",
		})
	}
	e.AddSource(ReferenceSource{
		Alias: alias,
		Path:  pattern,
		Items: items,
	})
	return nil
}

// fuzzyMatch reports whether the target matches the query (prefix + substring).
func fuzzyMatch(target, query string) bool {
	lower := strings.ToLower(target)
	if strings.HasPrefix(lower, query) {
		return true
	}
	return strings.Contains(lower, query)
}
