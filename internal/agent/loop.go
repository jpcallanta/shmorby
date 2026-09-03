package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"shmorby/internal/audit"
	ctxcomp "shmorby/internal/context"
	"shmorby/internal/health"
	"shmorby/internal/llm"
	"shmorby/internal/memory"
	"shmorby/internal/redact"
	"shmorby/internal/session"
	"shmorby/internal/tools"
)

// AgentEvent is emitted during tool execution for UI status updates.
type AgentEvent struct {
	Type   string // "tool-start", "tool-end", "tool-status"
	Name   string // tool name
	Info   string // command or status text
	Output string // tool output (only on tool-end)
	Status string // inference-generated status description (only on tool-status)
}

// StreamFunc receives text deltas from LLM streaming responses.
type StreamFunc func(delta string)

// AgentEventFunc receives events during agent execution.
type AgentEventFunc func(AgentEvent)

// ToolPermissionResponse is returned by the permission callback.
type ToolPermissionResponse int

const (
	PermDeny ToolPermissionResponse = iota
	PermAllow
	PermAllowAll
)

// ToolPermissionFunc is called when permission evaluates to "ask".
// Return PermAllow, PermDeny, or PermAllowAll.
type ToolPermissionFunc func(toolName, command, reason string) ToolPermissionResponse

// Runs a single chat turn: builds system prompt, retrieves relevant memory,
// sends user text to LLM with session history, and on success stores both
// user and assistant messages to session.
// ledgerCtx is pre-formatted ledger context for injection (empty if disabled).
// projectRoot is the resolved project directory for the env
// hint (empty = default).
func RunTurn(
	ctx context.Context,
	p llm.Provider,
	sess *session.Session,
	mode, scope, override, model, userText string,
	store memory.Store,
	retriever *memory.Retriever,
	compressor *ctxcomp.Compressor,
	modelInfo llm.ModelInfo,
	ledgerCtx string,
	projectRoot string,
) (string, error) {
	sys, err := SystemPrompt(mode, scope, override, projectRoot)
	if err != nil {
		return "", fmt.Errorf("build system prompt: %w", err)
	}

	// Compress session before LLM call if configured.
	if compressor != nil {
		// No tools in this path; reset any cached tool estimate
		// so a stale value is never reused (F1).
		compressor.SetRequestContext(sys, nil)
		if cErr := compressor.Compress(ctx, sess, modelInfo); cErr != nil {
			slog.Warn("compression failed", "err", cErr)
		}
	}

	// Retrieve relevant memory and inject as system context.
	var contextMsg string
	if retriever != nil {
		result, rErr := retriever.Retrieve(ctx, userText)
		if rErr == nil && len(result.Entries) > 0 {
			contextMsg = memory.FormatMemoryContext(
				result.Entries, retriever.ContextBudget(),
			)
		}
	}

	// Build messages from session history, then add current user message
	// for the request without persisting to session yet.
	history := sess.Messages()
	if contextMsg != "" {
		history = memory.InjectMemoryContext(history, contextMsg)
	}
	// Inject ledger context after memory context.
	if ledgerCtx != "" {
		history = memory.InjectMemoryContext(history, ledgerCtx)
	}
	msgs := make([]llm.Message, 0, len(history)+1)
	for _, m := range history {
		msgs = append(msgs, llm.Message{
			Role:    m.Role,
			Content: m.Content,
		})
	}
	msgs = append(msgs, llm.Message{
		Role:    "user",
		Content: userText,
	})

	req := llm.ChatRequest{
		Model:    model,
		System:   sys,
		Messages: msgs,
	}

	if err := checkContextWindow(modelInfo, compressor, &req, ctx, sess); err != nil {
		return "", err
	}

	resp, err := p.Chat(ctx, req)
	if err != nil {
		return "", fmt.Errorf("chat: %w", err)
	}

	// Log token usage for cost visibility when available.
	if resp.Usage.TotalTokens > 0 {
		slog.Debug("chat usage",
			"model", model,
			"prompt_tokens", resp.Usage.PromptTokens,
			"completion_tokens", resp.Usage.CompletionTokens,
			"total_tokens", resp.Usage.TotalTokens,
		)
	}

	// Only persist messages on successful chat.
	sess.Append("user", userText)
	sess.Append("assistant", resp.Text())

	return resp.Text(), nil
}

// Runs chat turns with tool execution loop up to max iterations.
// Buffers all messages locally and only persists on success,
// turnState holds the mutable state for a single turn's iteration loop.
// Both RunTurnWithTools and RunTurnWithToolsStream build one via
// initTurnState, then drive it with their respective LLM call style.
type turnState struct {
	sys             string
	toolDefs        []llm.ToolDef
	pending         []session.Message
	cacheablePrefix []llm.Message
	toolOverrides   map[string]bool
	degraded        []*health.Degraded
	iterStart       time.Time
	iterBudget      time.Duration
	maxIterations   int
}

// initTurnState builds the common setup shared by streaming and
// non-streaming turn runs: system prompt, tool defs, pending messages,
// memory context, cacheable prefix, and compressor registration.
func initTurnState(
	ctx context.Context,
	sess *session.Session,
	mode, scope, override, model, userText string,
	registry *tools.Registry,
	maxIterations int,
	shellEnabled bool,
	retriever *memory.Retriever,
	compressor *ctxcomp.Compressor,
	ledgerCtx, projectRoot string,
) (*turnState, error) {
	if maxIterations < 1 {
		maxIterations = 1
	}

	sys, err := SystemPrompt(mode, scope, override, projectRoot)
	if err != nil {
		return nil, fmt.Errorf("build system prompt: %w", err)
	}

	toolDefs := buildToolDefs(registry, mode, shellEnabled)

	pending := make([]session.Message, 0, 8)
	pending = append(pending, session.Message{
		Role:    "user",
		Content: userText,
	})

	memoryCtx := retrieveMemoryContext(ctx, userText, sess, retriever)
	baseHistory := buildBaseHistory(sess, memoryCtx, ledgerCtx)
	cacheablePrefix := buildCacheablePrefix(sys, baseHistory)

	// Marshal tool definitions once per turn: they are byte-identical
	// across iterations, so the compressor estimate is cached and
	// reused (F1: full request estimation).
	if compressor != nil {
		toolsJSON, mErr := json.Marshal(toolDefs)
		if mErr != nil {
			slog.Warn("marshal tool defs for compressor failed",
				"err", mErr)
		}

		compressor.SetRequestContext(sys, toolsJSON)
	}

	return &turnState{
		sys:             sys,
		toolDefs:        toolDefs,
		pending:         pending,
		cacheablePrefix: cacheablePrefix,
		toolOverrides:   make(map[string]bool),
		degraded:        nil,
		iterStart:       time.Now(),
		iterBudget:      time.Duration(maxIterations) * 240 * time.Second,
		maxIterations:   maxIterations,
	}, nil
}

// avoiding partial state on mid-loop LLM failure.
// Returns final assistant text or iteration-limit message.
// onEvent receives tool status updates; may be nil.
// permFunc is called when a tool's permission evaluates to "ask"
// (nil = always allow). toolRules are per-tool RuleSets from config.
// ledgerCtx is pre-formatted ledger context for injection (empty if disabled).
// projectRoot is the resolved project directory for the env
// hint (empty = default).
func RunTurnWithTools(
	ctx context.Context,
	p llm.Provider,
	sess *session.Session,
	mode, scope, override, model, userText string,
	registry *tools.Registry,
	maxIterations int,
	shellEnabled bool,
	store memory.Store,
	retriever *memory.Retriever,
	compressor *ctxcomp.Compressor,
	modelInfo llm.ModelInfo,
	onEvent AgentEventFunc,
	permFunc ToolPermissionFunc,
	toolRules map[string]*tools.RuleSet,
	statusGen *StatusGenerator,
	ledgerCtx string,
	projectRoot string,
) (string, error) {
	ts, err := initTurnState(ctx, sess, mode, scope, override, model, userText,
		registry, maxIterations, shellEnabled, retriever, compressor,
		ledgerCtx, projectRoot)
	if err != nil {
		return "", err
	}

	for i := 0; i < ts.maxIterations; i++ {
		if ts.iterBudget > 0 && time.Since(ts.iterStart) > ts.iterBudget {
			slog.Warn("agent iteration budget exceeded",
				"elapsed", time.Since(ts.iterStart),
				"budget", ts.iterBudget,
				"iteration", i,
			)
			break
		}
		// Compress session before LLM call if configured.
		if compressor != nil {
			if cErr := compressor.Compress(ctx, sess, modelInfo); cErr != nil {
				slog.Warn("compression failed", "err", cErr)
			}
		}

		msgs := buildIterationMessages(ts.cacheablePrefix, ts.pending)

		req := llm.ChatRequest{
			Model:    model,
			System:   ts.sys,
			Messages: msgs,
			Tools:    ts.toolDefs,
		}

		if err := checkContextWindow(modelInfo, compressor, &req, ctx, sess); err != nil {
			return "", err
		}

		resp, err := p.Chat(ctx, req)
		if err != nil {
			// No session writes made yet; caller can retry cleanly.
			return "", fmt.Errorf("chat: %w", err)
		}

		// Log token usage for cost visibility when available.
		if resp.Usage.TotalTokens > 0 {
			slog.Debug("chat usage",
				"model", model,
				"prompt_tokens", resp.Usage.PromptTokens,
				"completion_tokens", resp.Usage.CompletionTokens,
				"total_tokens", resp.Usage.TotalTokens,
			)
		}

		// No tool calls: final assistant text, flush everything.
		if len(resp.ToolCalls) == 0 {
			text := resp.Text()
			// Surface degraded tooling at top of response.
			if pfx := health.FormatPrefix(ts.degraded); pfx != "" {
				text = pfx + text
			}
			ts.pending = append(ts.pending, session.Message{
				Role:    "assistant",
				Content: text,
			})
			sess.AppendMessages(ts.pending)

			return text, nil
		}

		// Persist assistant message with tool calls in pending buffer.
		ts.pending = append(ts.pending, session.Message{
			Role:      "assistant",
			Content:   resp.Text(),
			ToolCalls: resp.ToolCalls,
		})

		for _, tc := range resp.ToolCalls {
			result := processToolCall(ctx, tc, registry,
				ts.toolOverrides, toolRules, permFunc,
				shellEnabled, mode, store, compressor,
				onEvent, statusGen, &ts.degraded)
			ts.pending = append(ts.pending, session.Message{
				Role:       "tool",
				Content:    result,
				ToolName:   tc.Name,
				ToolCallID: tc.ID,
			})
		}
	}

	// Iteration cap reached; include degraded note if any.
	text, err := finishIterationLimit(ctx, p, sess, ts.pending,
		ts.cacheablePrefix, model, ts.sys, modelInfo, ts.maxIterations)
	if err != nil {
		return "", err
	}
	if pfx := health.FormatPrefix(ts.degraded); pfx != "" {
		text = pfx + text
	}

	return text, nil
}

// RunTurnWithToolsStream is like RunTurnWithTools but uses ChatStream
// for progressive text output. Calls onDelta for each text/reasoning
// chunk as it arrives from the provider.
// ledgerCtx is pre-formatted ledger context for injection (empty if disabled).
// projectRoot is the resolved project directory for the env
// hint (empty = default).
func RunTurnWithToolsStream(
	ctx context.Context,
	p llm.Provider,
	sess *session.Session,
	mode, scope, override, model, userText string,
	registry *tools.Registry,
	maxIterations int,
	shellEnabled bool,
	store memory.Store,
	retriever *memory.Retriever,
	compressor *ctxcomp.Compressor,
	modelInfo llm.ModelInfo,
	onEvent AgentEventFunc,
	onDelta StreamFunc,
	permFunc ToolPermissionFunc,
	toolRules map[string]*tools.RuleSet,
	statusGen *StatusGenerator,
	ledgerCtx string,
	projectRoot string,
) (string, error) {
	// Stream variant safely handles nil registry for tool defs.
	registryForDefs := registry
	if registryForDefs == nil {
		registryForDefs = tools.NewRegistry()
	}

	ts, err := initTurnState(ctx, sess, mode, scope, override, model, userText,
		registryForDefs, maxIterations, shellEnabled, retriever, compressor,
		ledgerCtx, projectRoot)
	if err != nil {
		return "", err
	}

	for i := 0; i < ts.maxIterations; i++ {
		if ts.iterBudget > 0 && time.Since(ts.iterStart) > ts.iterBudget {
			slog.Warn("agent iteration budget exceeded (stream)",
				"elapsed", time.Since(ts.iterStart),
				"budget", ts.iterBudget,
				"iteration", i,
			)
			break
		}

		if compressor != nil {
			if cErr := compressor.Compress(ctx, sess, modelInfo); cErr != nil {
				slog.Warn("compression failed", "err", cErr)
			}
		}

		msgs := buildIterationMessages(ts.cacheablePrefix, ts.pending)

		req := llm.ChatRequest{
			Model:    model,
			System:   ts.sys,
			Messages: msgs,
			Tools:    ts.toolDefs,
		}

		if err := checkContextWindow(modelInfo, compressor, &req, ctx, sess); err != nil {
			return "", err
		}

		stream, sErr := p.ChatStream(ctx, req)
		if sErr != nil {
			return "", fmt.Errorf("chat: %w", sErr)
		}

		var text strings.Builder
		var toolCalls []llm.ToolCall

		for event := range stream {
			switch event.Type {
			case "text":
				text.WriteString(event.Delta)
				if onDelta != nil {
					onDelta(event.Delta)
				}
			case "reasoning":
				// Reasoning deltas are surfaced via onDelta
				// for UI display but NOT concatenated into the
				// persisted assistant content (CoT tokens
				// would leak into the session message).
				if onDelta != nil {
					onDelta(event.Delta)
				}
			case "usage":
				// Log token usage from stream_options for
				// cost visibility. The chunk contains JSON
				// with prompt/completion/total tokens.
				slog.Debug("stream usage",
					"model", model,
					"usage", event.Content,
				)
			case "tool-call":
				toolCalls = append(toolCalls, llm.ToolCall{
					ID:   event.ToolID,
					Name: event.Tool,
					Args: event.Content,
				})
			case "error":
				return "", fmt.Errorf("chat: %s", event.Delta)
			}
		}

		if len(toolCalls) == 0 {
			textStr := text.String()
			if pfx := health.FormatPrefix(ts.degraded); pfx != "" {
				textStr = pfx + textStr
			}
			ts.pending = append(ts.pending, session.Message{
				Role:    "assistant",
				Content: textStr,
			})
			sess.AppendMessages(ts.pending)

			return textStr, nil
		}

		ts.pending = append(ts.pending, session.Message{
			Role:      "assistant",
			Content:   text.String(),
			ToolCalls: toolCalls,
		})

		for _, tc := range toolCalls {
			result := processToolCall(ctx, tc, registry,
				ts.toolOverrides, toolRules, permFunc,
				shellEnabled, mode, store, compressor,
				onEvent, statusGen, &ts.degraded)
			ts.pending = append(ts.pending, session.Message{
				Role:       "tool",
				Content:    result,
				ToolName:   tc.Name,
				ToolCallID: tc.ID,
			})
		}
	}

	// Iteration cap reached; include degraded note if any.
	txt, err := finishIterationLimit(ctx, p, sess, ts.pending,
		ts.cacheablePrefix, model, ts.sys, modelInfo, ts.maxIterations)
	if err != nil {
		return "", err
	}
	if pfx := health.FormatPrefix(ts.degraded); pfx != "" {
		txt = pfx + txt
	}

	return txt, nil
}

// Filters tool schemas by agent mode and shell enabled flag, then
// converts them to LLM tool definitions. Always advertises non-shell
// tools even when shell is disabled.
func buildToolDefs(
	registry *tools.Registry,
	mode string,
	shellEnabled bool,
) []llm.ToolDef {
	schemas := registry.Schemas()
	if mode == "diagnose" {
		schemas = filterDiagnoseSchemas(schemas)
	}
	if mode == "chat" {
		schemas = filterChatSchemas(schemas)
	}
	if mode == "code" {
		schemas = filterCodeSchemas(schemas)
	}
	if !shellEnabled {
		filtered := make([]tools.ToolSchema, 0, len(schemas))
		for _, s := range schemas {
			if s.Name != "shell" {
				filtered = append(filtered, s)
			}
		}
		schemas = filtered
	}

	var defs []llm.ToolDef
	for _, ts := range schemas {
		defs = append(defs, llm.ToolDef{
			Name:        ts.Name,
			Description: ts.Description,
			Parameters:  ts.Parameters,
		})
	}

	return defs
}

// Retrieves relevant memory entries for the user text and formats
// them for injection. Returns empty string if no retriever or no
// results. Deduplicates against existing session messages to avoid
// redundant context.
func retrieveMemoryContext(
	ctx context.Context,
	userText string,
	sess *session.Session,
	retriever *memory.Retriever,
) string {
	if retriever == nil {
		return ""
	}

	result, rErr := retriever.Retrieve(ctx, userText)
	if rErr != nil || len(result.Entries) == 0 {
		return ""
	}

	deduped := memory.DedupMemoryContext(
		result.Entries, sess.Messages(),
	)

	return memory.FormatMemoryContext(
		deduped, retriever.ContextBudget(),
	)
}

// Returns session messages with memory and ledger context injected,
// forming the stable base for the cacheable prefix.
func buildBaseHistory(
	sess *session.Session,
	memoryCtx, ledgerCtx string,
) []session.Message {
	baseHistory := sess.Messages()
	if memoryCtx != "" {
		baseHistory = memory.InjectMemoryContext(
			baseHistory, memoryCtx,
		)
	}
	// Inject ledger context after memory context.
	if ledgerCtx != "" {
		baseHistory = memory.InjectMemoryContext(
			baseHistory, ledgerCtx,
		)
	}

	return baseHistory
}

// Constructs the message prefix that is byte-identical across all
// iterations within a single turn. Includes system prompt and base
// history (with memory/ledger). Used for provider prompt caching.
func buildCacheablePrefix(
	sys string,
	baseHistory []session.Message,
) []llm.Message {
	prefix := make([]llm.Message, 0, 1+len(baseHistory))
	prefix = append(prefix, llm.Message{
		Role:    "system",
		Content: sys,
	})

	for _, m := range baseHistory {
		prefix = append(prefix, llm.Message{
			Role:       m.Role,
			Content:    m.Content,
			ToolName:   m.ToolName,
			ToolCallID: m.ToolCallID,
			ToolCalls:  m.ToolCalls,
		})
	}

	return prefix
}

// Constructs the full message list for an LLM request by combining
// the cacheable prefix with pending messages.
func buildIterationMessages(
	prefix []llm.Message,
	pending []session.Message,
) []llm.Message {
	msgs := make([]llm.Message, len(prefix), len(prefix)+len(pending))
	copy(msgs, prefix)

	for _, m := range pending {
		msgs = append(msgs, llm.Message{
			Role:       m.Role,
			Content:    m.Content,
			ToolName:   m.ToolName,
			ToolCallID: m.ToolCallID,
			ToolCalls:  m.ToolCalls,
		})
	}

	return msgs
}

// Handles the full lifecycle of a single tool call: permission
// evaluation, execution, memory capture, error formatting, output
// compression, secret redaction, and event emission. Returns the
// final redacted result string to append to the pending messages.
// Any execution errors are already incorporated into the returned
// string. The UI event receives the raw (unredacted) result, while
// the returned string has secrets stripped via redact.SecretString
// so the LLM provider never sees credentials.
// degraded, when non-nil, collects structured degraded diagnostics
// for surfacing at the top of the final response.
//
// Key design: permission evaluation and tool execution are kept as
// sequential operations within this function rather than separate
// helpers because they share mutable state (toolOverrides) and the
// control flow (early returns on permission deny) is tightly coupled.
func processToolCall(
	ctx context.Context,
	tc llm.ToolCall,
	registry *tools.Registry,
	toolOverrides map[string]bool,
	toolRules map[string]*tools.RuleSet,
	permFunc ToolPermissionFunc,
	shellEnabled bool,
	mode string,
	store memory.Store,
	compressor *ctxcomp.Compressor,
	onEvent AgentEventFunc,
	statusGen *StatusGenerator,
	degraded *[]*health.Degraded,
) string {
	cmd := extractCommand(tc.Name, tc.Args)
	argsBytes := json.RawMessage(tc.Args)

	// Emit tool-start event.
	if onEvent != nil {
		onEvent(AgentEvent{
			Type: "tool-start",
			Name: tc.Name,
			Info: cmd,
		})
	}

	// Async status description generation: fire-and-forget.
	// The goroutine produces a short present-tense label and
	// emits a tool-status event when ready. Context checks
	// prevent goroutine leaks on cancellation.
	if statusGen != nil && onEvent != nil {
		go func(name, desc, command string) {
			// Exit early if context is already cancelled
			// to avoid unnecessary Generate calls.
			select {
			case <-ctx.Done():
				return
			default:
			}
			desc = statusGen.Generate(ctx, name, desc, command)
			if desc == "" {
				return
			}
			select {
			case <-ctx.Done():
			default:
				onEvent(AgentEvent{
					Type:   "tool-status",
					Name:   name,
					Status: desc,
				})
			}
		}(tc.Name, toolDescription(registry, tc.Name), cmd)
	}

	var result string
	var runErr error

	// Permission evaluation: check tool-level perm + rule set
	// before execution. When registry is nil or tool is not found,
	// short-circuit to error without further checks.
	if registry == nil {
		result = "error: tool not found"
	} else if tool, ok := registry.Lookup(tc.Name); ok {
		if !toolOverrides[tc.Name] {
			action, reason, rulePattern, ruleAction, pErr := tools.EvaluateToolPermission(
				tool.PermLevel(), cmd, toolRules[tc.Name],
			)
			if pErr != nil {
				result = "error: " + pErr.Error()
			} else if action == "ask" {
				// Default allow preserves v1 behavior
				// when no interactive func is wired.
				resp := PermAllow
				if permFunc != nil {
					resp = permFunc(tc.Name, cmd, reason)
				}
				switch resp {
				case PermDeny:
					result = fmt.Sprintf(
						"error: permission denied for %s: %s",
						tc.Name, cmd,
					)
				case PermAllowAll:
					toolOverrides[tc.Name] = true
				case PermAllow:
					// fall through to execute
				}
			}

			if auditLog := tools.AuditLoggerFrom(ctx); auditLog != nil {
				decision := "allow"
				if result != "" && strings.HasPrefix(result, "error: permission denied") {
					decision = "deny"
				}
				auditLog.LogPermission(audit.PermissionAudit{
					SessionID:   tools.SessionIDFrom(ctx),
					Tool:        tc.Name,
					Command:     string(tools.RedactArgs(argsBytes)),
					RulePattern: rulePattern,
					RuleAction:  ruleAction,
					Decision:    decision,
					Reason:      reason,
				})
			}
		}
	} else {
		result = "error: tool not found"
	}

	// Execute tool if no permission error.
	if result == "" && registry != nil {
		// Reject shell tool when not enabled; other
		// tools (ssh/sudo/aws) still work.
		if tc.Name == "shell" && !shellEnabled {
			result = "error: shell tool disabled " +
				"(shell_enabled=false)"
		} else if mode == "diagnose" &&
			(tc.Name == "shell" ||
				tc.Name == "sudo" || tc.Name == "ssh") {
			// CheckMutating guard for tools that accept a
			// command field in diagnose mode.
			var sa struct {
				Command string `json:"command"`
			}
			if uErr := json.Unmarshal(
				argsBytes, &sa,
			); uErr == nil && sa.Command != "" {
				if mErr := tools.CheckMutating(
					sa.Command,
				); mErr != nil {
					result = "error: " + mErr.Error()
				} else {
					result, runErr = registry.Run(
						ctx, tc.Name,
						argsBytes,
					)
				}
			} else {
				result = "error: diagnose mode " +
					"rejected " + tc.Name +
					" call with invalid or" +
					" empty command"
			}
		} else {
			result, runErr = registry.Run(
				ctx, tc.Name,
				argsBytes,
			)
		}
	}

	// Capture to memory store after tool execution. Failures
	// are stored with exit code 1 so outcome summaries
	// distinguish failures from successes (F2).
	// Heuristic: strings.HasPrefix(result, "error:") marks
	// permission denials / tool-not-found; a successful
	// command whose stdout begins with "error:" is a known
	// false-positive edge case (documented, accepted).
	captureMemory(store, tc, argsBytes, result, runErr)

	// Collect degraded diagnostics for prefix surfacing.
	if runErr != nil && degraded != nil {
		if d, ok := health.AsDegraded(runErr); ok {
			*degraded = append(*degraded, d)
		}
	}

	// Preserve partial output on error; else pure error string.
	if runErr != nil {
		if result != "" {
			result = result + "\nerror: " + runErr.Error()
		} else {
			result = "error: " + runErr.Error()
		}
	}

	// Compress large tool outputs.
	if compressor != nil {
		result = compressor.CompressToolOutput(result)
	}

	// Emit tool-end event with the raw result so the UI displays
	// the full tool output (including any secrets) to the user.
	if onEvent != nil {
		status := "done"
		if runErr != nil {
			status = "error: " + runErr.Error()
		}
		onEvent(AgentEvent{
			Type:   "tool-end",
			Name:   tc.Name,
			Info:   status,
			Output: result,
		})
	}

	// Redact secrets from tool output before returning to the LLM
	// context. The raw result is emitted to the UI above, but the
	// LLM must never see credentials captured in tool output such
	// as API keys, passwords, or tokens.
	return redact.SecretString(result)
}

// Records a tool execution result in the memory store. Failures
// are stored with exit code 1 so outcome summaries can distinguish
// failures from successes. The "command" field is extracted from
// tool args when available.
func captureMemory(
	store memory.Store,
	tc llm.ToolCall,
	argsBytes json.RawMessage,
	result string,
	runErr error,
) {
	if store == nil || result == "" {
		return
	}

	var parsed struct {
		Command string `json:"command"`
	}

	commandStr := tc.Args
	if json.Unmarshal(argsBytes, &parsed) == nil &&
		parsed.Command != "" {
		commandStr = parsed.Command
	}

	exitCode := 0
	if runErr != nil ||
		strings.HasPrefix(result, "error:") {
		exitCode = 1
	}

	memory.CaptureToolResult(
		store, memory.DefaultSessionID,
		tc.Name, commandStr, tc.Args, result, exitCode,
	)
}

// Handles the case where the tool loop exhausted all iterations.
// Appends a summary prompt, makes one final Chat without tools, and
// returns the summary. Falls back to a generic limit message if the
// summary LLM call fails.
func finishIterationLimit(
	ctx context.Context,
	p llm.Provider,
	sess *session.Session,
	pending []session.Message,
	cacheablePrefix []llm.Message,
	model, sys string,
	modelInfo llm.ModelInfo,
	maxIterations int,
) (string, error) {
	pending = append(pending, session.Message{
		Role:    "user",
		Content: MaxStepsPrompt,
	})

	msgs := buildIterationMessages(cacheablePrefix, pending)

	req := llm.ChatRequest{
		Model:    model,
		System:   sys,
		Messages: msgs,
	}

	// Apply MaxTokens via side effect; discard context-overflow error
	// intentionally — this path already falls back to a generic limit
	// message on any Chat failure below.
	_ = checkContextWindow(modelInfo, nil, &req, ctx, sess)

	resp, err := p.Chat(ctx, req)
	if err != nil {
		slog.Warn("summary LLM call failed, falling back to "+
			"generic limit message",
			"error", err,
		)
		reply := "Tool iteration limit reached (" +
			strconv.Itoa(maxIterations) + " iterations)."
		pending = append(pending, session.Message{
			Role:    "assistant",
			Content: reply,
		})
		sess.AppendMessages(pending)

		return reply, nil
	}

	pending = append(pending, session.Message{
		Role:    "assistant",
		Content: resp.Text(),
	})
	sess.AppendMessages(pending)

	return resp.Text(), nil
}

// Provides a rough upper-bound on total request tokens using a chars/4
// heuristic. Includes system, messages, and serialized tool definitions.
// The chars/4 heuristic is intentionally simple (no tiktoken dependency)
// since this is a safety check — overestimating is safe, and exact counts
// aren't needed to catch context overflow.
func estimateRequestTokens(sysPrompt string, msgs []llm.Message, tools []llm.ToolDef) int {
	total := (len(sysPrompt) + 3) / 4
	for _, m := range msgs {
		total += (len(m.Content) + 3) / 4
	}
	if len(tools) > 0 {
		data, err := json.Marshal(tools)
		if err != nil {
			total += 256 // conservative fallback per tool set
		} else {
			total += (len(data) + 3) / 4
		}
	}

	return total
}

// Verifies the estimated request fits within the model's context window,
// applies MaxTokens from ModelInfo, and if the estimate exceeds 90% of
// the window it tries emergency compression via the compressor. Returns
// a fatal error when compression is insufficient. The 90% threshold is a
// safety margin — below this the model typically handles mid-request
// truncation gracefully; above it the provider often returns a hard error.
func checkContextWindow(
	modelInfo llm.ModelInfo,
	compressor *ctxcomp.Compressor,
	req *llm.ChatRequest,
	ctx context.Context,
	sess *session.Session,
) error {
	// Apply output token cap from model metadata.
	if modelInfo.MaxOutputTokens > 0 {
		req.MaxTokens = modelInfo.MaxOutputTokens
	}

	// Bail early when no context window is known.
	if modelInfo.ContextWindow <= 0 {
		return nil
	}

	estimated := estimateRequestTokens(req.System, req.Messages, req.Tools)
	limit := int(float64(modelInfo.ContextWindow) * 0.9)

	// Within safe range — no action needed.
	if estimated <= limit {
		return nil
	}

	// Emergency compression: force one aggressive pass and re-estimate
	// after rebuilding messages from the compressed session. The pass
	// carries its mode override per-call — mutating the shared
	// compressor would race with concurrent subagents.
	if compressor != nil {
		slog.Warn("request exceeds 90% of context window, forcing compression",
			"estimated", estimated, "limit", limit)
		if cErr := compressor.CompressForced(ctx, sess, modelInfo); cErr != nil {
			slog.Warn("emergency compression failed", "err", cErr)
		}

		history := sess.Messages()
		rebuilt := make([]llm.Message, 0, len(history)+len(req.Messages))
		for _, m := range history {
			rebuilt = append(rebuilt, llm.Message{
				Role:       m.Role,
				Content:    m.Content,
				ToolName:   m.ToolName,
				ToolCallID: m.ToolCallID,
				ToolCalls:  m.ToolCalls,
			})
		}
		for i := len(history); i < len(req.Messages); i++ {
			rebuilt = append(rebuilt, req.Messages[i])
		}
		req.Messages = rebuilt
		estimated = estimateRequestTokens(req.System, req.Messages, req.Tools)
	}

	// Still over limit after compression — fatal.
	if estimated > limit {
		return fmt.Errorf(
			"estimated request tokens (%d) exceeds 90%% of context "+
				"window (%d); try /reset or adjust compression settings",
			estimated, modelInfo.ContextWindow,
		)
	}

	return nil
}

// Only returns schemas for tools allowed in diagnose mode: shell, ssh,
// sudo/aws (the latter only if registered), websearch/webfetch, and
// ledger_get (read-only, for consulting known-good state).
func filterDiagnoseSchemas(schemas []tools.ToolSchema) []tools.ToolSchema {
	allowed := map[string]bool{
		"shell":      true,
		"ssh":        true,
		"sudo":       true,
		"aws":        true,
		"websearch":  true,
		"webfetch":   true,
		"ledger_get": true,
	}
	filtered := make([]tools.ToolSchema, 0, len(schemas))
	for _, s := range schemas {
		if allowed[s.Name] {
			filtered = append(filtered, s)
		}
	}

	return filtered
}

// Only returns schemas for tools allowed in chat mode: websearch
// and webfetch (the latter only if registered).
func filterChatSchemas(schemas []tools.ToolSchema) []tools.ToolSchema {
	allowed := map[string]bool{
		"websearch": true,
		"webfetch":  true,
	}
	filtered := make([]tools.ToolSchema, 0, len(schemas))
	for _, s := range schemas {
		if allowed[s.Name] {
			filtered = append(filtered, s)
		}
	}

	return filtered
}

// Only returns schemas for tools allowed in code mode: file operations,
// search, shell, task, and web tools.
func filterCodeSchemas(schemas []tools.ToolSchema) []tools.ToolSchema {
	allowed := map[string]bool{
		"file_read":  true,
		"file_edit":  true,
		"file_write": true,
		"find":       true,
		"grep":       true,
		"shell":      true,
		"task":       true,
		"websearch":  true,
		"webfetch":   true,
	}
	filtered := make([]tools.ToolSchema, 0, len(schemas))
	for _, s := range schemas {
		if allowed[s.Name] {
			filtered = append(filtered, s)
		}
	}

	return filtered
}

// toolDescription looks up a tool's Description() from the registry.
// Returns empty string if not found.
func toolDescription(registry *tools.Registry, name string) string {
	if registry == nil {
		return ""
	}
	t, ok := registry.Lookup(name)
	if !ok {
		return ""
	}
	return t.Description()
}

// ExtractCommand returns a human-readable command string from a
// tool call's arguments. Used for permission prompts and events.
func extractCommand(toolName, argsJSON string) string {
	argsBytes := []byte(argsJSON)
	switch toolName {
	// shell, sudo, and ssh all carry a "command" JSON field.
	case tools.ToolShell, tools.ToolSudo, tools.ToolSSH:
		var sa struct {
			Command string `json:"command"`
		}
		if json.Unmarshal(argsBytes, &sa) == nil {
			return sa.Command
		}
	case tools.ToolAWS:
		var sa struct {
			Args []string `json:"args"`
		}
		if json.Unmarshal(argsBytes, &sa) == nil && len(sa.Args) > 0 {
			return "aws " + strings.Join(sa.Args, " ")
		}
	// File tools carry a "path" JSON field.
	case tools.ToolFileRead, tools.ToolFileEdit, tools.ToolFileWrite:
		var sa struct {
			Path string `json:"path"`
		}
		if json.Unmarshal(argsBytes, &sa) == nil && sa.Path != "" {
			return toolName + " " + sa.Path
		}
	case tools.ToolGrep:
		var sa struct {
			Pattern string `json:"pattern"`
			Path    string `json:"path"`
		}
		if json.Unmarshal(argsBytes, &sa) == nil && sa.Pattern != "" {
			if sa.Path != "" {
				return "grep " + sa.Pattern + " " + sa.Path
			}
			return "grep " + sa.Pattern
		}
	case tools.ToolFind:
		var sa struct {
			Pattern string `json:"pattern"`
			Path    string `json:"path"`
		}
		if json.Unmarshal(argsBytes, &sa) == nil && sa.Pattern != "" {
			if sa.Path != "" {
				return "find " + sa.Pattern + " " + sa.Path
			}
			return "find " + sa.Pattern
		}
	}
	return toolName
}
