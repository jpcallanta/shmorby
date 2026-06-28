// Package history provides an input history ring buffer.
package history

// History is a ring buffer for input history.
type History struct {
	entries []string
	maxSize int
	cursor  int
}

// New creates a history buffer with the given max size.
func New(maxSize int) *History {
	if maxSize <= 0 {
		maxSize = 100
	}
	return &History{
		entries: make([]string, 0, maxSize),
		maxSize: maxSize,
		cursor:  0,
	}
}

// Add appends an entry. Silently drops oldest if at capacity.
func (h *History) Add(entry string) {
	if entry == "" {
		return
	}
	if len(h.entries) >= h.maxSize {
		h.entries = h.entries[1:]
	}
	h.entries = append(h.entries, entry)
	h.cursor = len(h.entries)
}

// Older moves back one entry. Returns the entry and true, or ("", false) at
// the oldest boundary.
func (h *History) Older() (string, bool) {
	if h.cursor <= 0 || len(h.entries) == 0 {
		return "", false
	}
	h.cursor--
	return h.entries[h.cursor], true
}

// Newer moves forward one entry. Returns the entry and true, or ("", false)
// at the newest boundary.
func (h *History) Newer() (string, bool) {
	if h.cursor >= len(h.entries) {
		return "", false
	}
	if h.cursor == len(h.entries)-1 {
		h.cursor = len(h.entries)
		return "", true
	}
	h.cursor++
	return h.entries[h.cursor], true
}

// Entries returns all stored entries.
func (h *History) Entries() []string {
	out := make([]string, len(h.entries))
	copy(out, h.entries)
	return out
}

// Size returns the number of stored entries.
func (h *History) Size() int {
	return len(h.entries)
}

// AtNewest reports whether cursor is at the newest (blank) position.
func (h *History) AtNewest() bool {
	return h.cursor >= len(h.entries)
}
