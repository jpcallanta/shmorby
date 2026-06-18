package history

import "strings"

// ReverseISearch implements a ctrl+r reverse-i-search overlay.
type ReverseISearch struct {
	history  *History
	query    string
	matches  []string
	selected int
	visible  bool
}

// NewReverseISearch creates a search overlay over the given history.
func NewReverseISearch(h *History) *ReverseISearch {
	return &ReverseISearch{
		history: h,
	}
}

// Toggle shows or hides the search popup.
func (s *ReverseISearch) Toggle() {
	s.visible = !s.visible
	if s.visible {
		s.query = ""
		s.selected = 0
		s.matches = s.computeMatches()
	}
}

// Visible reports whether the search is active.
func (s *ReverseISearch) Visible() bool {
	return s.visible
}

// Dismiss hides the search popup.
func (s *ReverseISearch) Dismiss() {
	s.visible = false
	s.query = ""
	s.matches = nil
	s.selected = 0
}

// Query returns the current search query.
func (s *ReverseISearch) Query() string {
	return s.query
}

// SetQuery updates the search query and recomputes matches.
func (s *ReverseISearch) SetQuery(q string) {
	s.query = q
	s.matches = s.computeMatches()
	if s.selected >= len(s.matches) {
		s.selected = 0
	}
}

// AddRune appends a rune to the query and updates matches.
func (s *ReverseISearch) AddRune(r rune) {
	s.SetQuery(s.query + string(r))
}

// CycleForward moves to the next match.
func (s *ReverseISearch) CycleForward() {
	if len(s.matches) > 0 {
		s.selected = (s.selected + 1) % len(s.matches)
	}
}

// CycleReverse moves to the previous match.
func (s *ReverseISearch) CycleReverse() {
	if len(s.matches) > 0 {
		s.selected = (s.selected - 1 + len(s.matches)) % len(s.matches)
	}
}

// Selected returns the currently selected history entry, or "".
func (s *ReverseISearch) Selected() string {
	if len(s.matches) == 0 {
		return ""
	}
	if s.selected >= len(s.matches) {
		s.selected = len(s.matches) - 1
	}
	return s.matches[s.selected]
}

// Matches returns the current match list.
func (s *ReverseISearch) Matches() []string {
	out := make([]string, len(s.matches))
	copy(out, s.matches)
	return out
}

// SelectedIndex returns the index of the selected match.
func (s *ReverseISearch) SelectedIndex() int {
	return s.selected
}

func (s *ReverseISearch) computeMatches() []string {
	if s.query == "" {
		all := s.history.Entries()
		// Return in reverse order (newest first)
		var rev []string
		for i := len(all) - 1; i >= 0; i-- {
			rev = append(rev, all[i])
		}
		return rev
	}
	q := strings.ToLower(s.query)
	all := s.history.Entries()
	var matches []string
	// Search from newest to oldest
	for i := len(all) - 1; i >= 0; i-- {
		if strings.Contains(strings.ToLower(all[i]), q) {
			matches = append(matches, all[i])
		}
	}
	return matches
}
