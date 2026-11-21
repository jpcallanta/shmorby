package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"shmorby/internal/backend"
	"shmorby/internal/xuuid"
)

type backendTurnResult struct {
	err error
}

func (r *REPL) runBackendTurn(
	ctx context.Context,
	text string,
	onEvent AgentEventFunc,
	onDelta StreamFunc,
) (string, error) {
	if r.Backend == nil {
		return r.runLocalTurn(ctx, text, onEvent, onDelta)
	}
	if r.Session == nil {
		return "", errors.New("backend unavailable")
	}

	inputID := InputIDFrom(ctx)
	if inputID == "" {
		id, err := newInputID()
		if err != nil {
			return "", err
		}
		inputID = id
		ctx = WithInputID(ctx, inputID)
	}

	eventCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	events, err := r.Backend.Subscribe(eventCtx, backend.EventFilter{
		SessionID: r.Session.ID(),
		InputID:   inputID,
	})
	if err != nil {
		return "", fmt.Errorf("subscribe backend events: %w", err)
	}

	permission := r.ToolPermFunc
	if permission == nil && r.ToolRules != nil {
		permission = r.toolPermissionFunc
	}
	if r.Orchestrator != nil {
		r.Orchestrator.PermFunc = permission
	}

	result := make(chan backendTurnResult, 1)
	go func() {
		_, err := r.Backend.SubmitInput(ctx, r.Session.ID(), backend.Input{
			ID:                backend.InputID(inputID),
			Text:              text,
			ModelInfo:         r.ModelInfo,
			Stream:            onDelta != nil,
			Mode:              r.Mode,
			Model:             r.Model,
			Override:          r.Override,
			PermissionHandler: AdaptPermissionHandler(permission),
		})
		result <- backendTurnResult{err: err}
	}()

	var reply string
	gotReply := false
	gotResult := false
	for !gotResult || !gotReply {
		select {
		case event, ok := <-events:
			if !ok {
				return "", errors.New("backend event stream closed")
			}
			switch event.Type {
			case backend.EventTextDelta, backend.EventReasoningDelta:
				var data backend.DeltaEventData
				if json.Unmarshal(event.Data, &data) == nil && onDelta != nil {
					onDelta(data.Delta)
				}
			case backend.EventToolStarted,
				backend.EventToolProgress,
				backend.EventToolEnded:
				var data backend.ToolEventData
				if json.Unmarshal(event.Data, &data) != nil || onEvent == nil {
					continue
				}
				typeName := "tool-start"
				if event.Type == backend.EventToolProgress {
					typeName = "tool-status"
				} else if event.Type == backend.EventToolEnded {
					typeName = "tool-end"
				}
				onEvent(AgentEvent{
					Type: typeName, Name: data.Tool,
					Info: data.Command, Status: data.Status,
					Output: data.Output,
				})
			case backend.EventMessageCreated:
				var data backend.MessageEventData
				if err := json.Unmarshal(event.Data, &data); err != nil {
					return "", fmt.Errorf("decode backend message: %w", err)
				}
				if data.Role == "assistant" {
					reply = data.Content
					gotReply = true
				}
			case backend.EventInputCancelled:
				var data struct {
					Error string `json:"error"`
				}
				_ = json.Unmarshal(event.Data, &data)
				if data.Error != "" {
					return "", errors.New(data.Error)
				}
				return "", context.Canceled
			}
		case done := <-result:
			gotResult = true
			if done.err != nil {
				return "", done.err
			}
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}

	return reply, nil
}

// Runs a standalone REPL turn when no application backend is injected.
func (r *REPL) runLocalTurn(
	ctx context.Context,
	text string,
	onEvent AgentEventFunc,
	onDelta StreamFunc,
) (string, error) {
	permission := r.ToolPermFunc
	if permission == nil && r.ToolRules != nil {
		permission = r.toolPermissionFunc
	}
	if r.Orchestrator != nil {
		r.Orchestrator.PermFunc = permission
	}

	if r.Registry == nil {
		return RunTurn(
			ctx, r.Provider, r.Session,
			r.Mode, r.Scope, r.Override, r.Model, text,
			r.Store, r.Retriever, r.Compressor, r.ModelInfo,
			r.LedgerCtx, r.ProjectRoot,
		)
	}
	if onDelta != nil {
		reply, err := RunTurnWithToolsStream(
			ctx, r.Provider, r.Session,
			r.Mode, r.Scope, r.Override, r.Model, text,
			r.Registry, r.MaxToolIter, r.ShellEnabled,
			r.Store, r.Retriever, r.Compressor, r.ModelInfo,
			onEvent, onDelta, permission, r.ToolRules,
			r.StatusGen, r.LedgerCtx, r.ProjectRoot,
		)
		if err == nil || !strings.Contains(err.Error(), "streaming not") {
			return reply, err
		}
	}

	return RunTurnWithTools(
		ctx, r.Provider, r.Session,
		r.Mode, r.Scope, r.Override, r.Model, text,
		r.Registry, r.MaxToolIter, r.ShellEnabled,
		r.Store, r.Retriever, r.Compressor, r.ModelInfo,
		onEvent, permission, r.ToolRules,
		r.StatusGen, r.LedgerCtx, r.ProjectRoot,
	)
}

// Converts a frontend approval prompt into the backend-neutral callback.
func AdaptPermissionHandler(
	permission ToolPermissionFunc,
) backend.PermissionHandler {
	if permission == nil {
		return nil
	}

	return func(request backend.PermissionRequest) backend.PermissionDecision {
		resolution := ToolPermissionResolution{}
		response := permission(ToolPermissionRequest{
			Context:         request.Context,
			SessionID:       request.SessionID,
			InputID:         request.InputID,
			ExecutionID:     request.ExecutionID,
			Tool:            request.Tool,
			Command:         request.Command,
			CommandRedacted: request.CommandRedacted,
			RulePattern:     request.RulePattern,
			RuleAction:      request.RuleAction,
			Reason:          request.Reason,
			Resolution:      &resolution,
		})
		copyPermissionResolution(request.Resolution, resolution)
		switch response {
		case PermAllow:
			return backend.PermissionAllow
		case PermAllowAll:
			return backend.PermissionAllowAll
		default:
			return backend.PermissionDeny
		}
	}
}

// Converts a backend callback for agent turns.
func AdaptBackendPermissionHandler(
	handler backend.PermissionHandler,
) ToolPermissionFunc {
	if handler == nil {
		return nil
	}

	return func(request ToolPermissionRequest) ToolPermissionResponse {
		resolution := backend.PermissionResolution{}
		response := handler(backend.PermissionRequest{
			Context:         request.Context,
			SessionID:       request.SessionID,
			InputID:         request.InputID,
			ExecutionID:     request.ExecutionID,
			Tool:            request.Tool,
			Command:         request.Command,
			CommandRedacted: request.CommandRedacted,
			RulePattern:     request.RulePattern,
			RuleAction:      request.RuleAction,
			Reason:          request.Reason,
			Resolution:      &resolution,
		})
		if request.Resolution != nil {
			request.Resolution.ApprovalID = resolution.ApprovalID
			request.Resolution.DecidedBy = resolution.DecidedBy
			request.Resolution.DecisionSource = resolution.DecisionSource
			request.Resolution.Scope = resolution.Scope
		}
		switch response {
		case backend.PermissionAllow:
			return PermAllow
		case backend.PermissionAllowAll:
			return PermAllowAll
		default:
			return PermDeny
		}
	}
}

func copyPermissionResolution(
	destination *backend.PermissionResolution,
	source ToolPermissionResolution,
) {
	if destination == nil {
		return
	}
	destination.ApprovalID = source.ApprovalID
	destination.DecidedBy = source.DecidedBy
	destination.DecisionSource = source.DecisionSource
	destination.Scope = source.Scope
}

func newInputID() (string, error) {
	return xuuid.New()
}
