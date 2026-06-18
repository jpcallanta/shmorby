// Package navigation implements TUI navigation features.
package navigation

// ModeSwitcher cycles between agent modes.
type ModeSwitcher struct {
	modes   []string
	current int
}

// NewModeSwitcher creates a switcher cycling operate ↔ diagnose.
func NewModeSwitcher() *ModeSwitcher {
	return &ModeSwitcher{
		modes:   []string{"operate", "diagnose"},
		current: 0,
	}
}

// CycleForward advances to the next mode.
func (m *ModeSwitcher) CycleForward() {
	m.current = (m.current + 1) % len(m.modes)
}

// CycleReverse goes to the previous mode.
func (m *ModeSwitcher) CycleReverse() {
	m.current = (m.current - 1 + len(m.modes)) % len(m.modes)
}

// Current returns the active mode string.
func (m *ModeSwitcher) Current() string {
	return m.modes[m.current]
}

// SetCurrent sets the current mode by name. Returns false if unknown.
func (m *ModeSwitcher) SetCurrent(name string) bool {
	for i, mode := range m.modes {
		if mode == name {
			m.current = i
			return true
		}
	}
	return false
}

// Modes returns all available modes.
func (m *ModeSwitcher) Modes() []string {
	out := make([]string, len(m.modes))
	copy(out, m.modes)
	return out
}
