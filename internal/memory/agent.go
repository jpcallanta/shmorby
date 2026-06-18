package memory

import (
	"fmt"
	"strings"
	"time"

	"shmorby/internal/session"
)

// Formats a list of memory entries into a context string for the LLM.
// Returns empty string when entries is empty.
func FormatMemoryContext(entries []MemoryEntry) string {
	if len(entries) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("Relevant past actions:\n")

	for _, e := range entries {
		ts := e.Timestamp.Format("2006-01-02")
		status := "success"
		if e.ExitCode != 0 {
			status = fmt.Sprintf("exit %d", e.ExitCode)
		}
		b.WriteString(fmt.Sprintf(
			"- [%s] %s: %s → %s",
			ts, e.Tool, e.Command, status,
		))
		if len(e.Tags) > 0 {
			b.WriteString(fmt.Sprintf(" (%s)", strings.Join(e.Tags, ", ")))
		}
		b.WriteString("\n")
	}

	b.WriteString("\nUse this context if relevant to the current request.")

	return b.String()
}

// Injects a memory context string as a system message before the first
// user role message, or at the end if no user message is found.
// Returns the original slice unchanged when contextMsg is empty.
func InjectMemoryContext(
	messages []session.Message, contextMsg string,
) []session.Message {
	if contextMsg == "" {
		return messages
	}

	result := make([]session.Message, 0, len(messages)+1)
	inserted := false

	for _, m := range messages {
		if !inserted && m.Role == "user" {
			result = append(result, session.Message{
				Role:    "system",
				Content: contextMsg,
			})
			inserted = true
		}
		result = append(result, m)
	}

	if !inserted {
		result = append(result, session.Message{
			Role:    "system",
			Content: contextMsg,
		})
	}

	return result
}

// DefaultSessionID is the session identifier used when no real session
// tracking is available.
const DefaultSessionID = "default"

// Captures a tool execution to the memory store if the store is non-nil
// and auto-capture is enabled.
func CaptureToolResult(
	store Store,
	sessionID, tool, command, args, result string, exitCode int,
) {
	if store == nil || !store.AutoCaptureEnabled() {
		return
	}

	timestamp := time.Now()
	truncResult := truncateResult(result)
	tags := extractTags(command, store.TagRules())

	entry := MemoryEntry{
		ID:        newUUID(),
		Timestamp: timestamp,
		SessionID: sessionID,
		Tool:      tool,
		Command:   command,
		Args:      args,
		Result:    truncResult,
		ExitCode:  exitCode,
		Tags:      tags,
	}

	if err := store.Insert(entry); err != nil {
		// Non-fatal; log and continue.
		return
	}
}
