package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// PermissionChoice is the user's response to a permission prompt.
type PermissionChoice int

const (
	PermissionAllow PermissionChoice = iota
	PermissionDeny
	PermissionAllowAll
)

// PermissionPrompt models an inline permission prompt shown to the user.
type PermissionPrompt struct {
	Tool    string
	Command string
	Reason  string
	Rule    string
	Choice  chan PermissionChoice
}

// NewPermissionPrompt creates a prompt ready for display.
func NewPermissionPrompt(tool, command, reason, rule string) PermissionPrompt {
	return PermissionPrompt{
		Tool:    tool,
		Command: command,
		Reason:  reason,
		Rule:    rule,
		Choice:  make(chan PermissionChoice, 1),
	}
}

// renderPermissionPrompt renders the permission box between separators.
func (m Model) renderPermissionPrompt(width int) string {
	if m.permission == nil {
		return ""
	}

	borderWidth := width - 4
	if borderWidth < 10 {
		borderWidth = 10
	}
	var b strings.Builder
	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.theme.PermPrompt.GetForeground()).
		Width(borderWidth).
		Padding(0, 1)

	content := fmt.Sprintf(
		"command: %s\nreason: %s\nrule: %s",
		m.permission.Command,
		m.permission.Reason,
		m.permission.Rule,
	)
	allow := m.theme.PermAllow.Render("[y] allow")
	deny := m.theme.PermDeny.Render("[n] deny")
	all := m.theme.PermAllow.Render("[a] allow all like this")
	content += "\n" + allow + "   " + deny + "   " + all

	b.WriteString(border.Render(content))
	return b.String()
}

// renderHaltPrompt renders the stop-all confirmation box.
func (m Model) renderHaltPrompt(width int) string {
	borderWidth := width - 4
	if borderWidth < 10 {
		borderWidth = 10
	}
	var b strings.Builder
	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.theme.PermPrompt.GetForeground()).
		Width(borderWidth).
		Padding(0, 1)

	allow := m.theme.PermDeny.Render("[y] halt all operations")
	deny := m.theme.PermAllow.Render("[n] continue")
	content := "halt everything?\n" + allow + "   " + deny

	b.WriteString(border.Render(content))
	return b.String()
}
