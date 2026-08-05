package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"shmorby/internal/audit"
	ctxcomp "shmorby/internal/context"
	"shmorby/internal/llm"
	"shmorby/internal/memory"
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
func RunTurn(
	ctx context.Context,
	p llm.Provider,
	sess *session.Session,
	mode, scope, override, model, userText string,
	store memory.Store,
	retriever *memory.Retriever,
	compressor *ctxcomp.Compressor,
	modelInfo llm.ModelInfo,
) (string, error) {
	sys, err := SystemPrompt(mode, scope, override)
	if err != nil {
		return "", fmt.Errorf("build system prompt: %w", err)
	}

	// Compress session before LLM call if configured.
	if compressor != nil {
		if cErr := compressor.Compress(ctx, sess, modelInfo); cErr != nil {
			slog.Warn("compression failed", "err", cErr)
		}
	}

	// Retrieve relevant memory and inject as system context.
	var contextMsg string
	if retriever != nil {
		result, rErr := retriever.Retrieve(ctx, userText)
		if rErr == nil && len(result.Entries) > 0 {
			contextMsg = memory.FormatMemoryContext(result.Entries)
		}
	}

	// Build messages from session history, then add current user message
	// for the request without persisting to session yet.
	history := sess.Messages()
	if contextMsg != "" {
		history = memory.InjectMemoryContext(history, contextMsg)
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

	// Only persist messages on successful chat.
	sess.Append("user", userText)
	sess.Append("assistant", resp.Text())

	return resp.Text(), nil
}

// Runs chat turns with tool execution loop up to max iterations.
// Buffers all messages locally and only persists on success,
// avoiding partial state on mid-loop LLM failure.
// Returns final assistant text or iteration-limit message.
// onEvent receives tool status updates; may be nil.
// permFunc is called when a tool's permission evaluates to "ask"
// (nil = always allow). toolRules are per-tool RuleSets from config.
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
) (string, error) {
	// Ensure at least one iteration runs.
	if maxIterations < 1 {
		maxIterations = 1
	}

	sys, err := SystemPrompt(mode, scope, override)
	if err != nil {
		return "", fmt.Errorf("build system prompt: %w", err)
	}

	// Filter tool schemas by mode; always advertise non-shell tools
	// even when shell is disabled.
	var toolDefs []llm.ToolDef
	schemas := registry.Schemas()
	if mode == "diagnose" {
		schemas = filterDiagnoseSchemas(schemas)
	}
	if mode == "chat" {
		schemas = filterChatSchemas(schemas)
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
	for _, ts := range schemas {
		toolDefs = append(toolDefs, llm.ToolDef{
			Name:        ts.Name,
			Description: ts.Description,
			Parameters:  ts.Parameters,
		})
	}

	// Buffer all messages for this turn; flush to session only on success.
	pending := make([]session.Message, 0, 8)
	pending = append(pending, session.Message{
		Role:    "user",
		Content: userText,
	})

	// Retrieve relevant memory before tool loop.
	var memoryCtx string
	if retriever != nil {
		result, rErr := retriever.Retrieve(ctx, userText)
		if rErr == nil && len(result.Entries) > 0 {
			deduped := memory.DedupMemoryContext(
				result.Entries, sess.Messages(),
			)
			memoryCtx = memory.FormatMemoryContext(deduped)
		}
	}

	// Build base history once: session with memory context injected.
	baseHistory := sess.Messages()
	if memoryCtx != "" {
		baseHistory = memory.InjectMemoryContext(
			baseHistory, memoryCtx,
		)
	}

	// Build cacheable prefix once: system + base history (with memory)
	// + tool schemas. Byte-identical across iterations for provider
	// prompt caching.
	var cacheablePrefix []llm.Message
	cacheablePrefix = append(cacheablePrefix, llm.Message{
		Role:    "system",
		Content: sys,
	})
	for _, m := range baseHistory {
		cacheablePrefix = append(cacheablePrefix, llm.Message{
			Role:       m.Role,
			Content:    m.Content,
			ToolName:   m.ToolName,
			ToolCallID: m.ToolCallID,
			ToolCalls:  m.ToolCalls,
		})
	}

	// Session-level overrides persist across iterations within one turn.
	toolOverrides := make(map[string]bool)

	for i := 0; i < maxIterations; i++ {
		// Compress session before LLM call if configured.
		if compressor != nil {
			if cErr := compressor.Compress(ctx, sess, modelInfo); cErr != nil {
				slog.Warn("compression failed", "err", cErr)
			}
		}

		// Build messages: cacheable prefix + pending.
		msgs := make([]llm.Message, len(cacheablePrefix),
			len(cacheablePrefix)+len(pending))
		copy(msgs, cacheablePrefix)
		for _, m := range pending {
			msgs = append(msgs, llm.Message{
				Role:       m.Role,
				Content:    m.Content,
				ToolName:   m.ToolName,
				ToolCallID: m.ToolCallID,
				ToolCalls:  m.ToolCalls,
			})
		}

		req := llm.ChatRequest{
			Model:    model,
			System:   sys,
			Messages: msgs,
			Tools:    toolDefs,
		}

		if err := checkContextWindow(modelInfo, compressor, &req, ctx, sess); err != nil {
			return "", err
		}

		resp, err := p.Chat(ctx, req)
		if err != nil {
			// No session writes made yet; caller can retry cleanly.
			return "", fmt.Errorf("chat: %w", err)
		}

		// No tool calls: final assistant text, flush everything.
		if len(resp.ToolCalls) == 0 {
			pending = append(pending, session.Message{
				Role:    "assistant",
				Content: resp.Text(),
			})
			sess.AppendMessages(pending)

			return resp.Text(), nil
		}

		// Persist assistant message with tool calls in pending buffer.
		pending = append(pending, session.Message{
			Role:      "assistant",
			Content:   resp.Text(),
			ToolCalls: resp.ToolCalls,
		})

		for _, tc := range resp.ToolCalls {
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
			// emits a tool-status event when ready.
			if statusGen != nil && onEvent != nil {
				go func(name, desc, command string) {
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

			// Permission evaluation (phase 26): check tool-level
			// perm + rule set before execution.
			tool, ok := registry.Lookup(tc.Name)
			if !ok {
				result = "error: tool not found"
			} else if !toolOverrides[tc.Name] {
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

			// Execute tool if no permission error.
			if result == "" {
				// Reject shell tool when not enabled; other
				// tools (ssh/sudo/aws) still work.
				if tc.Name == "shell" && !shellEnabled {
					result = "error: shell tool disabled " +
						"(shell_enabled=false)"
				} else if mode == "diagnose" &&
					tc.Name == "shell" {
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
							"rejected shell call with " +
							"invalid or empty command"
					}
				} else {
					result, runErr = registry.Run(
						ctx, tc.Name,
						argsBytes,
					)
				}
			}

			// Capture to memory store after successful execution.
			if store != nil && runErr == nil && result != "" {
				var parsed struct {
					Command string `json:"command"`
				}
				commandStr := tc.Args
				if json.Unmarshal(argsBytes, &parsed) == nil &&
					parsed.Command != "" {
					commandStr = parsed.Command
				}
				memory.CaptureToolResult(
					store, memory.DefaultSessionID,
					tc.Name, commandStr, tc.Args, result, 0,
				)
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

			// Emit tool-end event.
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

			pending = append(pending, session.Message{
				Role:       "tool",
				Content:    result,
				ToolName:   tc.Name,
				ToolCallID: tc.ID,
			})
		}
	}

	// Iteration limit reached: append summary request as user message,
	// make one final Chat without tools, return summary.
	pending = append(pending, session.Message{
		Role:    "user",
		Content: MaxStepsPrompt,
	})

	msgs := make([]llm.Message, len(cacheablePrefix),
		len(cacheablePrefix)+len(pending))
	copy(msgs, cacheablePrefix)
	for _, m := range pending {
		msgs = append(msgs, llm.Message{
			Role:       m.Role,
			Content:    m.Content,
			ToolName:   m.ToolName,
			ToolCallID: m.ToolCallID,
			ToolCalls:  m.ToolCalls,
		})
	}

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

// RunTurnWithToolsStream is like RunTurnWithTools but uses ChatStream
// for progressive text output. Calls onDelta for each text/reasoning
// chunk as it arrives from the provider.
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
) (string, error) {
	if maxIterations < 1 {
		maxIterations = 1
	}

	sys, err := SystemPrompt(mode, scope, override)
	if err != nil {
		return "", fmt.Errorf("build system prompt: %w", err)
	}

	var toolDefs []llm.ToolDef
	if registry != nil {
		schemas := registry.Schemas()
		if mode == "diagnose" {
			schemas = filterDiagnoseSchemas(schemas)
		}
		if mode == "chat" {
			schemas = filterChatSchemas(schemas)
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
		for _, ts := range schemas {
			toolDefs = append(toolDefs, llm.ToolDef{
				Name:        ts.Name,
				Description: ts.Description,
				Parameters:  ts.Parameters,
			})
		}
	}

	pending := make([]session.Message, 0, 8)
	pending = append(pending, session.Message{
		Role:    "user",
		Content: userText,
	})

	var memoryCtx string
	if retriever != nil {
		result, rErr := retriever.Retrieve(ctx, userText)
		if rErr == nil && len(result.Entries) > 0 {
			deduped := memory.DedupMemoryContext(
				result.Entries, sess.Messages(),
			)
			memoryCtx = memory.FormatMemoryContext(deduped)
		}
	}

	baseHistory := sess.Messages()
	if memoryCtx != "" {
		baseHistory = memory.InjectMemoryContext(
			baseHistory, memoryCtx,
		)
	}

	var cacheablePrefix []llm.Message
	cacheablePrefix = append(cacheablePrefix, llm.Message{
		Role:    "system",
		Content: sys,
	})
	for _, m := range baseHistory {
		cacheablePrefix = append(cacheablePrefix, llm.Message{
			Role:       m.Role,
			Content:    m.Content,
			ToolName:   m.ToolName,
			ToolCallID: m.ToolCallID,
			ToolCalls:  m.ToolCalls,
		})
	}

	toolOverrides := make(map[string]bool)

	for i := 0; i < maxIterations; i++ {
		if compressor != nil {
			if cErr := compressor.Compress(ctx, sess, modelInfo); cErr != nil {
				slog.Warn("compression failed", "err", cErr)
			}
		}

		msgs := make([]llm.Message, len(cacheablePrefix),
			len(cacheablePrefix)+len(pending))
		copy(msgs, cacheablePrefix)
		for _, m := range pending {
			msgs = append(msgs, llm.Message{
				Role:       m.Role,
				Content:    m.Content,
				ToolName:   m.ToolName,
				ToolCallID: m.ToolCallID,
				ToolCalls:  m.ToolCalls,
			})
		}

		req := llm.ChatRequest{
			Model:    model,
			System:   sys,
			Messages: msgs,
			Tools:    toolDefs,
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
			case "text", "reasoning":
				text.WriteString(event.Delta)
				if onDelta != nil {
					onDelta(event.Delta)
				}
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
			pending = append(pending, session.Message{
				Role:    "assistant",
				Content: text.String(),
			})
			sess.AppendMessages(pending)

			return text.String(), nil
		}

		pending = append(pending, session.Message{
			Role:      "assistant",
			Content:   text.String(),
			ToolCalls: toolCalls,
		})

		for _, tc := range toolCalls {
			cmd := extractCommand(tc.Name, tc.Args)
			argsBytes := json.RawMessage(tc.Args)

			if onEvent != nil {
				onEvent(AgentEvent{
					Type: "tool-start",
					Name: tc.Name,
					Info: cmd,
				})
			}

			// Async status description generation: fire-and-forget.
			if statusGen != nil && onEvent != nil {
				go func(name, desc, command string) {
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

			if result == "" && registry != nil {
				if tc.Name == "shell" && !shellEnabled {
					result = "error: shell tool disabled " +
						"(shell_enabled=false)"
				} else if mode == "diagnose" &&
					tc.Name == "shell" {
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
							"rejected shell call with " +
							"invalid or empty command"
					}
				} else {
					result, runErr = registry.Run(
						ctx, tc.Name,
						argsBytes,
					)
				}
			}

			if store != nil && runErr == nil && result != "" {
				var parsed struct {
					Command string `json:"command"`
				}
				commandStr := tc.Args
				if json.Unmarshal(argsBytes, &parsed) == nil &&
					parsed.Command != "" {
					commandStr = parsed.Command
				}
				memory.CaptureToolResult(
					store, memory.DefaultSessionID,
					tc.Name, commandStr, tc.Args, result, 0,
				)
			}

			if runErr != nil {
				if result != "" {
					result = result + "\nerror: " + runErr.Error()
				} else {
					result = "error: " + runErr.Error()
				}
			}

			if compressor != nil {
				result = compressor.CompressToolOutput(result)
			}

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

			pending = append(pending, session.Message{
				Role:       "tool",
				Content:    result,
				ToolName:   tc.Name,
				ToolCallID: tc.ID,
			})
		}
	}

	pending = append(pending, session.Message{
		Role:    "user",
		Content: MaxStepsPrompt,
	})

	msgs := make([]llm.Message, len(cacheablePrefix),
		len(cacheablePrefix)+len(pending))
	copy(msgs, cacheablePrefix)
	for _, m := range pending {
		msgs = append(msgs, llm.Message{
			Role:       m.Role,
			Content:    m.Content,
			ToolName:   m.ToolName,
			ToolCallID: m.ToolCallID,
			ToolCalls:  m.ToolCalls,
		})
	}

	req := llm.ChatRequest{
		Model:    model,
		System:   sys,
		Messages: msgs,
	}

	// Apply MaxTokens via side effect; discard context-overflow error
	// intentionally — this path already falls back to a generic limit
	// message on any Chat failure below.
	_ = checkContextWindow(modelInfo, nil, &req, ctx, sess)

	resp, cErr := p.Chat(ctx, req)
	if cErr != nil {
		slog.Warn("summary LLM call failed, falling back to "+
			"generic limit message",
			"error", cErr,
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

	// Emergency compression: temporarily switch to aggressive mode and
	// re-estimate after rebuilding messages from the compressed session.
	if compressor != nil {
		slog.Warn("request exceeds 90% of context window, forcing compression",
			"estimated", estimated, "limit", limit)
		origMode := compressor.Config().Mode
		compressor.SetMode("aggressive")
		if cErr := compressor.Compress(ctx, sess, modelInfo); cErr != nil {
			slog.Warn("emergency compression failed", "err", cErr)
		}
		compressor.SetMode(origMode)

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
// sudo/aws (the latter only if registered), and websearch/webfetch.
func filterDiagnoseSchemas(schemas []tools.ToolSchema) []tools.ToolSchema {
	allowed := map[string]bool{
		"shell":     true,
		"ssh":       true,
		"sudo":      true,
		"aws":       true,
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
	case "shell", "sudo":
		var sa struct {
			Command string `json:"command"`
		}
		if json.Unmarshal(argsBytes, &sa) == nil {
			return sa.Command
		}
	case "ssh":
		var sa struct {
			Command string `json:"command"`
		}
		if json.Unmarshal(argsBytes, &sa) == nil {
			return sa.Command
		}
	case "aws":
		var sa struct {
			Args []string `json:"args"`
		}
		if json.Unmarshal(argsBytes, &sa) == nil && len(sa.Args) > 0 {
			return "aws " + strings.Join(sa.Args, " ")
		}
	}
	return toolName
}
