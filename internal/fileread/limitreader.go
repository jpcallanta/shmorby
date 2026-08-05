// Package fileread provides file I/O utilities with safety limits.
package fileread

import (
	"fmt"
	"io"
	"os"
)

// MaxFileReadSize is the default maximum size for file reads (10 MB).
// Prevents OOM when reading user-provided or config files.
const MaxFileReadSize = 10 * 1024 * 1024

// ReadFileLimited reads a file with a size limit to prevent resource
// exhaustion from unexpectedly large files. Returns an error if the
// file exceeds the limit.
func ReadFileLimited(path string, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		maxBytes = MaxFileReadSize
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Check file size first to avoid reading if too large.
	fi, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}
	if fi.Size() > maxBytes {
		return nil, fmt.Errorf(
			"file %s exceeds size limit (%d bytes > %d bytes limit)",
			path, fi.Size(), maxBytes,
		)
	}

	// Use LimitedReader as a safety net in case Stat is inaccurate
	// (e.g., /proc files, symlinks).
	lr := &io.LimitedReader{R: f, N: maxBytes + 1}
	data, err := io.ReadAll(lr)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf(
			"file %s exceeds size limit (%d bytes > %d bytes limit)",
			path, len(data), maxBytes,
		)
	}

	return data, nil
}
