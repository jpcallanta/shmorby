// Package util provides shared utility functions used across packages.
package util

import (
	"os"
	"path/filepath"
	"strings"
)

// ExpandPath expands a leading ~/ to the user's home directory.
// Returns the path unchanged when it doesn't start with ~/
// or when the home directory can't be resolved.
func ExpandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}

	return path
}
