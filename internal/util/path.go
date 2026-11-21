// Package util provides shared utility functions used across packages.
package util

import (
	"bytes"
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

// Detects PNG and JPEG data by magic bytes. Returns the media type
// ("image/png", "image/jpeg") and file extension (".png", ".jpg");
// both empty when data is not a recognized image format.
func SniffImageType(data []byte) (mediaType, ext string) {
	switch {
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):

		return "image/png", ".png"
	case bytes.HasPrefix(data, []byte("\xff\xd8\xff")):

		return "image/jpeg", ".jpg"
	}

	return "", ""
}
