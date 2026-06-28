package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// Handles /memory slash command and its subcommands in the TUI model.
// Appends output to m.output directly.
func (m *Model) handleMemoryCommand(parts []string) {
	sub := ""
	if len(parts) > 1 {
		sub = parts[1]
	}

	switch sub {
	case "":
		m.recentMemoryCmd(20)

	case "search":
		if len(parts) < 3 {
			m.output = append(m.output, outputEntry{
				kind: "error",
				text: "usage: /memory search <query>",
			})
			m.syncViewport()

			return
		}
		query := strings.Join(parts[2:], " ")
		m.searchMemoryCmd(query)

	case "forget":
		if len(parts) < 3 {
			m.output = append(m.output, outputEntry{
				kind: "error",
				text: "usage: /memory forget <id>",
			})
			m.syncViewport()

			return
		}
		m.forgetMemoryCmd(parts[2])

	case "clear":
		m.confirmClearMemoryCmd()

	case "stats":
		m.memoryStatsCmd()

	default:
		m.output = append(m.output, outputEntry{
			kind: "error",
			text: fmt.Sprintf("unknown /memory subcommand: %s", sub),
		})
		m.syncViewport()
	}
}

// recentMemoryCmd shows the most recent memory entries.
func (m *Model) recentMemoryCmd(limit int) {
	if m.memoryStore == nil {
		m.output = append(m.output, outputEntry{
			kind: "error",
			text: "Memory store not available.",
		})
		m.syncViewport()

		return
	}

	entries, err := m.memoryStore.List(limit, 0)
	if err != nil {
		m.output = append(m.output, outputEntry{
			kind: "error",
			text: fmt.Sprintf("list memory: %v", err),
		})
		m.syncViewport()

		return
	}

	if len(entries) == 0 {
		m.output = append(m.output, outputEntry{
			kind: "agent",
			text: "No memory entries.",
		})
		m.syncViewport()

		return
	}

	var b strings.Builder
	for _, e := range entries {
		b.WriteString(fmt.Sprintf("[%s] %s: %s\n",
			e.Timestamp.Format("2006-01-02 15:04"),
			e.Tool, e.Command,
		))
	}
	b.WriteString(fmt.Sprintf("(%d entries)", len(entries)))

	m.output = append(m.output, outputEntry{
		kind: "agent",
		text: b.String(),
	})
	m.syncViewport()
}

// searchMemoryCmd runs a vector or keyword memory search.
func (m *Model) searchMemoryCmd(query string) {
	if m.retriever == nil {
		m.output = append(m.output, outputEntry{
			kind: "error",
			text: "Memory search not available.",
		})
		m.syncViewport()

		return
	}

	result, err := m.retriever.Retrieve(
		context.Background(), query,
	)
	if err != nil {
		m.output = append(m.output, outputEntry{
			kind: "error",
			text: fmt.Sprintf("search memory: %v", err),
		})
		m.syncViewport()

		return
	}

	entries := result.Entries

	if len(entries) == 0 {
		m.output = append(m.output, outputEntry{
			kind: "agent",
			text: "No matching entries.",
		})
		m.syncViewport()

		return
	}

	var b strings.Builder
	for _, e := range entries {
		b.WriteString(fmt.Sprintf("[%s] %s: %s → result: %s\n",
			e.Timestamp.Format("2006-01-02 15:04"),
			e.Tool, e.Command,
			strconv.Itoa(len(e.Result))+" bytes",
		))
	}
	b.WriteString(fmt.Sprintf("(%d results)", len(entries)))

	m.output = append(m.output, outputEntry{
		kind: "agent",
		text: b.String(),
	})
	m.syncViewport()
}

// forgetMemoryCmd deletes a specific memory entry by ID.
func (m *Model) forgetMemoryCmd(id string) {
	if m.memoryStore == nil {
		m.output = append(m.output, outputEntry{
			kind: "error",
			text: "Memory store not available.",
		})
		m.syncViewport()

		return
	}

	if err := m.memoryStore.Delete(id); err != nil {
		m.output = append(m.output, outputEntry{
			kind: "error",
			text: fmt.Sprintf("delete memory: %v", err),
		})
		m.syncViewport()

		return
	}

	m.output = append(m.output, outputEntry{
		kind: "agent",
		text: fmt.Sprintf("Deleted entry %s.", id),
	})
	m.syncViewport()
}

// confirmClearMemoryCmd sets a permission prompt asking the user to
// confirm wiping all memory entries.
func (m *Model) confirmClearMemoryCmd() {
	if m.memoryStore == nil {
		m.output = append(m.output, outputEntry{
			kind: "error",
			text: "Memory store not available.",
		})
		m.syncViewport()

		return
	}

	count, err := m.memoryStore.Count()
	if err != nil {
		m.output = append(m.output, outputEntry{
			kind: "error",
			text: fmt.Sprintf("count memory: %v", err),
		})
		m.syncViewport()

		return
	}

	if count == 0 {
		m.output = append(m.output, outputEntry{
			kind: "agent",
			text: "No entries to clear.",
		})
		m.syncViewport()

		return
	}

	prompt := NewPermissionPrompt(
		"memory",
		"/memory clear",
		"wipe all memory entries",
		"user confirmation",
	)
	m.permission = &prompt
	m.pendingClearMemory = true
}

// executeMemoryClear performs the actual deletion after confirmation.
func (m *Model) executeMemoryClear() {
	entries, err := m.memoryStore.List(100000, 0)
	if err != nil {
		m.output = append(m.output, outputEntry{
			kind: "error",
			text: fmt.Sprintf("list memory: %v", err),
		})
		m.syncViewport()

		return
	}

	for _, e := range entries {
		_ = m.memoryStore.Delete(e.ID)
	}

	m.output = append(m.output, outputEntry{
		kind: "agent",
		text: fmt.Sprintf("Cleared %d entries.", len(entries)),
	})
	m.syncViewport()
}

// memoryStatsCmd shows entry count, storage stats, and retrieval statistics.
func (m *Model) memoryStatsCmd() {
	if m.memoryStore == nil {
		m.output = append(m.output, outputEntry{
			kind: "error",
			text: "Memory store not available.",
		})
		m.syncViewport()

		return
	}

	count, err := m.memoryStore.Count()
	if err != nil {
		m.output = append(m.output, outputEntry{
			kind: "error",
			text: fmt.Sprintf("count memory: %v", err),
		})
		m.syncViewport()

		return
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Entries: %d\n", count))

	// Show retrieval stats if retriever is available.
	if m.retriever != nil {
		stats := m.retriever.Stats()
		total := stats.Hits + stats.Misses
		hitRate := 0
		if total > 0 {
			hitRate = stats.Hits * 100 / total
		}
		b.WriteString(fmt.Sprintf("Retrievals: %d hits, %d misses (%d%% hit rate)\n",
			stats.Hits, stats.Misses, hitRate))
	}

	m.output = append(m.output, outputEntry{
		kind: "agent",
		text: b.String(),
	})
	m.syncViewport()
}

// Handles /context slash command and its subcommands.
func (m *Model) handleContextCommand(args string) {
	parts := strings.SplitN(args, " ", 2)
	sub := parts[0]

	switch sub {
	case "":
		m.contextInfoCmd()
	case "compress":
		m.manualCompressCmd()
	case "stats":
		m.contextStatsCmd()
	case "model":
		m.contextModelCmd()
	default:
		m.output = append(m.output, outputEntry{
			kind: "error",
			text: fmt.Sprintf("unknown /context subcommand: %s", sub),
		})
		m.syncViewport()
	}
}

// contextInfoCmd shows token usage, model context limit, and compression stats.
func (m *Model) contextInfoCmd() {
	m.updateCtxStats()

	if m.ctxStats == nil {
		m.output = append(m.output, outputEntry{
			kind: "error",
			text: "Context compression not configured.",
		})
		m.syncViewport()

		return
	}

	providerName := "none"
	if m.provider != nil {
		providerName = m.provider.Name()
	}
	pct := 0
	if m.ctxStats.ContextWindow > 0 {
		pct = int(float64(m.ctxStats.EstimatedTokens) /
			float64(m.ctxStats.ContextWindow) * 100)
	}

	thresholdPct := int(m.compressor.Config().Threshold * 100)

	var b strings.Builder
	b.WriteString("Context status:\n")
	b.WriteString(fmt.Sprintf("  Model: %s (%s)\n", m.model, providerName))
	b.WriteString(fmt.Sprintf("  Context window: %s tokens", formatTokens(m.ctxStats.ContextWindow)))
	if m.ctxStats.Fallback {
		b.WriteString(" (API-verified)\n")
	} else {
		b.WriteString("\n")
	}
	b.WriteString(fmt.Sprintf("  Estimated tokens: %s (%d%%)\n",
		formatTokens(m.ctxStats.EstimatedTokens), pct))
	b.WriteString(fmt.Sprintf("  Compression threshold: %d%%\n", thresholdPct))
	b.WriteString(fmt.Sprintf("  Compressions this session: %d\n", m.ctxStats.Compressions))
	b.WriteString(fmt.Sprintf("  Mode: %s\n", m.ctxStats.Mode))

	m.output = append(m.output, outputEntry{
		kind: "agent",
		text: b.String(),
	})
	m.syncViewport()
}

// manualCompressCmd manually triggers context compression.
func (m *Model) manualCompressCmd() {
	if m.compressor == nil || m.session == nil {
		m.output = append(m.output, outputEntry{
			kind: "error",
			text: "Context compression not configured.",
		})
		m.syncViewport()

		return
	}

	if err := m.compressor.Compress(context.Background(), m.session, m.modelInfo); err != nil {
		m.output = append(m.output, outputEntry{
			kind: "error",
			text: fmt.Sprintf("Compression failed: %v", err),
		})
		m.syncViewport()

		return
	}

	m.updateCtxStats()

	m.output = append(m.output, outputEntry{
		kind: "agent",
		text: "Context compressed.",
	})
	m.syncViewport()
}

// contextStatsCmd shows offloaded messages and storage usage.
func (m *Model) contextStatsCmd() {
	m.updateCtxStats()

	if m.ctxStats == nil {
		m.output = append(m.output, outputEntry{
			kind: "error",
			text: "Context compression not configured.",
		})
		m.syncViewport()

		return
	}

	storageMB := float64(m.ctxStats.StorageUsedBytes) / (1024 * 1024)

	var b strings.Builder
	b.WriteString("Memory offloading:\n")
	b.WriteString(fmt.Sprintf("  Total offloaded messages: %d\n", m.ctxStats.OffloadedMessages))
	b.WriteString(fmt.Sprintf("  Storage used: %.1f MB\n", storageMB))

	m.output = append(m.output, outputEntry{
		kind: "agent",
		text: b.String(),
	})
	m.syncViewport()
}

// contextModelCmd shows the current model's detected context window.
func (m *Model) contextModelCmd() {
	cw := m.modelInfo.ContextWindow
	cwSource := "provider API"
	if cw == 0 {
		if m.compressor != nil {
			cw = m.compressor.Config().FallbackContextWindow
		} else {
			cw = 8192
		}
		cwSource = "fallback"
	}

	toolSupport := "yes"
	if !m.modelInfo.SupportsTools {
		toolSupport = "no"
	}

	tokenizerInfo := "heuristic (no tiktoken mapping)"

	var b strings.Builder
	b.WriteString("Model info:\n")
	b.WriteString(fmt.Sprintf("  Name: %s\n", m.model))
	b.WriteString(fmt.Sprintf("  Context window: %s (source: %s)\n", formatTokens(cw), cwSource))
	b.WriteString(fmt.Sprintf("  Max output: %s\n", formatTokens(m.modelInfo.MaxOutputTokens)))
	b.WriteString(fmt.Sprintf("  Supports tools: %s\n", toolSupport))
	b.WriteString(fmt.Sprintf("  Tokenizer: %s\n", tokenizerInfo))

	m.output = append(m.output, outputEntry{
		kind: "agent",
		text: b.String(),
	})
	m.syncViewport()
}
