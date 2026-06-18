// Package palette provides a command palette with fuzzy matching.
package palette

import (
	"strings"
)

// CommandItem is a single command in the palette.
type CommandItem struct {
	Name        string
	Slash       string
	Description string
	Shortcut    string
	Action      func()
}

// CommandPalette provides a fuzzy-searchable command list.
type CommandPalette struct {
	items    []CommandItem
	filter   string
	selected int
	visible  bool
}

// New creates a command palette.
func New() *CommandPalette {
	return &CommandPalette{
		selected: 0,
	}
}

// SetItems replaces the command list.
func (p *CommandPalette) SetItems(items []CommandItem) {
	p.items = items
}

// AddItem registers a command.
func (p *CommandPalette) AddItem(item CommandItem) {
	p.items = append(p.items, item)
}

// Toggle shows or hides the palette. When showing, resets filter.
func (p *CommandPalette) Toggle() {
	p.visible = !p.visible
	if p.visible {
		p.filter = ""
		p.selected = 0
	}
}

// Visible reports whether the palette is shown.
func (p *CommandPalette) Visible() bool {
	return p.visible
}

// Dismiss hides the palette.
func (p *CommandPalette) Dismiss() {
	p.visible = false
	p.filter = ""
}

// SetFilter updates the search filter string.
func (p *CommandPalette) SetFilter(filter string) {
	p.filter = filter
	p.selected = 0
}

// Filter returns the current filter text.
func (p *CommandPalette) Filter() string {
	return p.filter
}

// MoveDown advances the selection.
func (p *CommandPalette) MoveDown() {
	matches := p.Filtered()
	if len(matches) > 0 {
		p.selected = (p.selected + 1) % len(matches)
	}
}

// MoveUp goes back in the selection.
func (p *CommandPalette) MoveUp() {
	matches := p.Filtered()
	if len(matches) > 0 {
		p.selected = (p.selected - 1 + len(matches)) % len(matches)
	}
}

// Selected returns the currently selected item, or nil.
func (p *CommandPalette) Selected() *CommandItem {
	matches := p.Filtered()
	if len(matches) == 0 {
		return nil
	}
	if p.selected >= len(matches) {
		p.selected = len(matches) - 1
	}
	return &matches[p.selected]
}

// SelectedIndex returns the index of the selected item.
func (p *CommandPalette) SelectedIndex() int {
	return p.selected
}

// Execute runs the selected command's action. Returns true if executed.
func (p *CommandPalette) Execute() bool {
	item := p.Selected()
	if item == nil || item.Action == nil {
		return false
	}
	p.visible = false
	p.filter = ""
	item.Action()
	return true
}

// Filtered returns items matching the current filter.
func (p *CommandPalette) Filtered() []CommandItem {
	if p.filter == "" {
		out := make([]CommandItem, len(p.items))
		copy(out, p.items)
		return out
	}
	q := strings.ToLower(p.filter)
	var matches []CommandItem
	for _, item := range p.items {
		if fuzzyMatch(item.Name, q) || fuzzyMatch(item.Description, q) || fuzzyMatch(item.Slash, q) {
			matches = append(matches, item)
		}
	}
	return matches
}

// fuzzyMatch reports whether target matches query via prefix or substring.
func fuzzyMatch(target, query string) bool {
	lower := strings.ToLower(target)
	return strings.HasPrefix(lower, query) || strings.Contains(lower, query)
}
