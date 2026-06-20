package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	ctxcomp "shmorby/internal/context"
	"shmorby/internal/llm"
	"shmorby/internal/memory"
	"shmorby/internal/session"
	"shmorby/internal/tools"
)

// AgentEvent is emitted during tool execution for UI status updates.
type AgentEvent struct {
	Type   string // "tool-start", "tool-end"
	Name   string // tool name
	Info   string // command or status text
	Output string // tool output (only on tool-end)
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
			memoryCtx = memory.FormatMemoryContext(result.Entries)
		}
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

		// Build request from persisted session history + pending messages.
		history := sess.Messages()
		if memoryCtx != "" {
			history = memory.InjectMemoryContext(history, memoryCtx)
		}
		msgs := make([]llm.Message, 0, len(history)+len(pending))
		for _, m := range history {
			msgs = append(msgs, llm.Message{
				Role:       m.Role,
				Content:    m.Content,
				ToolName:   m.ToolName,
				ToolCallID: m.ToolCallID,
				ToolCalls:  m.ToolCalls,
			})
		}
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

			// Emit tool-start event.
			if onEvent != nil {
				onEvent(AgentEvent{
					Type: "tool-start",
					Name: tc.Name,
					Info: cmd,
				})
			}

			var result string
			var runErr error

			// Permission evaluation (phase 26): check tool-level
			// perm + rule set before execution.
			tool, ok := registry.Lookup(tc.Name)
			if !ok {
				result = "error: tool not found"
			} else if !toolOverrides[tc.Name] {
				action, reason, pErr := tools.EvaluateToolPermission(
					tool.PermLevel(), cmd, toolRules[tc.Name],
				)
				if pErr != nil && action != "ask" {
					result = "error: " + pErr.Error()
				}
				if action == "ask" {
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
						[]byte(tc.Args), &sa,
					); uErr == nil && sa.Command != "" {
						if mErr := tools.CheckMutating(
							sa.Command,
						); mErr != nil {
							result = "error: " + mErr.Error()
						} else {
							result, runErr = registry.Run(
								ctx, tc.Name,
								json.RawMessage(tc.Args),
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
						json.RawMessage(tc.Args),
					)
				}
			}

			// Capture to memory store after successful execution.
			if store != nil && runErr == nil && result != "" {
				var parsed struct {
					Command string `json:"command"`
				}
				commandStr := tc.Args
				if json.Unmarshal([]byte(tc.Args), &parsed) == nil &&
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

	history := sess.Messages()
	if memoryCtx != "" {
		history = memory.InjectMemoryContext(history, memoryCtx)
	}
	msgs := make([]llm.Message, 0, len(history)+len(pending))
	for _, m := range history {
		msgs = append(msgs, llm.Message{
			Role:       m.Role,
			Content:    m.Content,
			ToolName:   m.ToolName,
			ToolCallID: m.ToolCallID,
			ToolCalls:  m.ToolCalls,
		})
	}
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
			memoryCtx = memory.FormatMemoryContext(result.Entries)
		}
	}

	toolOverrides := make(map[string]bool)

	for i := 0; i < maxIterations; i++ {
		if compressor != nil {
			if cErr := compressor.Compress(ctx, sess, modelInfo); cErr != nil {
				slog.Warn("compression failed", "err", cErr)
			}
		}

		history := sess.Messages()
		if memoryCtx != "" {
			history = memory.InjectMemoryContext(history, memoryCtx)
		}
		msgs := make([]llm.Message, 0, len(history)+len(pending))
		for _, m := range history {
			msgs = append(msgs, llm.Message{
				Role:       m.Role,
				Content:    m.Content,
				ToolName:   m.ToolName,
				ToolCallID: m.ToolCallID,
				ToolCalls:  m.ToolCalls,
			})
		}
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

			if onEvent != nil {
				onEvent(AgentEvent{
					Type: "tool-start",
					Name: tc.Name,
					Info: cmd,
				})
			}

			var result string
			var runErr error

			if registry == nil {
				result = "error: tool not found"
			} else if tool, ok := registry.Lookup(tc.Name); ok {
				if !toolOverrides[tc.Name] {
					action, reason, pErr := tools.EvaluateToolPermission(
						tool.PermLevel(), cmd, toolRules[tc.Name],
					)
					if pErr != nil && action != "ask" {
						result = "error: " + pErr.Error()
					}
					if action == "ask" {
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
						[]byte(tc.Args), &sa,
					); uErr == nil && sa.Command != "" {
						if mErr := tools.CheckMutating(
							sa.Command,
						); mErr != nil {
							result = "error: " + mErr.Error()
						} else {
							result, runErr = registry.Run(
								ctx, tc.Name,
								json.RawMessage(tc.Args),
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
						json.RawMessage(tc.Args),
					)
				}
			}

			if store != nil && runErr == nil && result != "" {
				var parsed struct {
					Command string `json:"command"`
				}
				commandStr := tc.Args
				if json.Unmarshal([]byte(tc.Args), &parsed) == nil &&
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

	history := sess.Messages()
	if memoryCtx != "" {
		history = memory.InjectMemoryContext(history, memoryCtx)
	}
	msgs := make([]llm.Message, 0, len(history)+len(pending))
	for _, m := range history {
		msgs = append(msgs, llm.Message{
			Role:       m.Role,
			Content:    m.Content,
			ToolName:   m.ToolName,
			ToolCallID: m.ToolCallID,
			ToolCalls:  m.ToolCalls,
		})
	}
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

// Only returns schemas for tools allowed in diagnose mode: shell, ssh,
// and sudo/aws (the latter only if registered).
func filterDiagnoseSchemas(schemas []tools.ToolSchema) []tools.ToolSchema {
	allowed := map[string]bool{
		"shell": true,
		"ssh":   true,
		"sudo":  true,
		"aws":   true,
	}
	filtered := make([]tools.ToolSchema, 0, len(schemas))
	for _, s := range schemas {
		if allowed[s.Name] {
			filtered = append(filtered, s)
		}
	}

	return filtered
}

// ExtractCommand returns a human-readable command string from a
// tool call's arguments. Used for permission prompts and events.
func extractCommand(toolName, argsJSON string) string {
	switch toolName {
	case "shell", "sudo":
		var sa struct {
			Command string `json:"command"`
		}
		if json.Unmarshal([]byte(argsJSON), &sa) == nil {
			return sa.Command
		}
	case "ssh":
		var sa struct {
			Command string `json:"command"`
		}
		if json.Unmarshal([]byte(argsJSON), &sa) == nil {
			return sa.Command
		}
	case "aws":
		var sa struct {
			Args []string `json:"args"`
		}
		if json.Unmarshal([]byte(argsJSON), &sa) == nil && len(sa.Args) > 0 {
			return "aws " + strings.Join(sa.Args, " ")
		}
	}
	return toolName
}
