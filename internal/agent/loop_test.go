package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"shmorby/internal/llm"
	"shmorby/internal/session"
	"shmorby/internal/tools"
)

// fakeProvider is a test double that records requests and returns canned
// responses.
type fakeProvider struct {
	name  string
	reply string
	calls []llm.ChatRequest
}

// Returns the provider name.
func (f *fakeProvider) Name() string { return f.name }

// Records the request and returns a canned response.
func (f *fakeProvider) Chat(
	ctx context.Context, req llm.ChatRequest,
) (llm.ChatResponse, error) {
	f.calls = append(f.calls, req)

	return llm.ChatResponse{
		Message: llm.Message{
			Role:    "assistant",
			Content: f.reply,
		},
	}, nil
}

// Streams a chat response (not implemented in test double).
func (f *fakeProvider) ChatStream(
	_ context.Context, _ llm.ChatRequest,
) (<-chan llm.StreamEvent, error) {
	return nil, fmt.Errorf("fake: streaming not yet supported")
}

// Returns model info (not implemented in test double).
func (f *fakeProvider) ModelInfo(
	_ context.Context, _ string,
) (llm.ModelInfo, error) {
	return llm.ModelInfo{}, nil
}

// TestRunTurn_TwoTurns_RetainsContext checks two turns keep session context.
func TestRunTurn_TwoTurns_RetainsContext(t *testing.T) {
	p := &fakeProvider{name: "fake", reply: "ACK"}
	s := session.New()

	_, err := RunTurn(
		context.Background(), p, s,
		"operate", "", "", "", "hello",
		nil, nil, nil, llm.ModelInfo{},
	)
	if err != nil {
		t.Fatalf("turn 1: %v", err)
	}

	_, err = RunTurn(
		context.Background(), p, s,
		"operate", "", "", "", "world",
		nil, nil, nil, llm.ModelInfo{},
	)
	if err != nil {
		t.Fatalf("turn 2: %v", err)
	}

	msgs := s.Messages()
	if len(msgs) != 4 {
		t.Fatalf("want 4 messages, got %d", len(msgs))
	}

	if msgs[0].Role != "user" || msgs[0].Content != "hello" {
		t.Errorf("msg[0]: want user/hello, got %s/%s",
			msgs[0].Role, msgs[0].Content)
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != "ACK" {
		t.Errorf("msg[1]: want assistant/ACK, got %s/%s",
			msgs[1].Role, msgs[1].Content)
	}
	if msgs[2].Role != "user" || msgs[2].Content != "world" {
		t.Errorf("msg[2]: want user/world, got %s/%s",
			msgs[2].Role, msgs[2].Content)
	}
	if msgs[3].Role != "assistant" || msgs[3].Content != "ACK" {
		t.Errorf("msg[3]: want assistant/ACK, got %s/%s",
			msgs[3].Role, msgs[3].Content)
	}

	// Verify second request contained both prior messages.
	if len(p.calls) != 2 {
		t.Fatalf("want 2 calls, got %d", len(p.calls))
	}
	if len(p.calls[1].Messages) != 3 {
		t.Fatalf("want 3 messages in turn 2 request, got %d",
			len(p.calls[1].Messages))
	}
}

// TestRunTurn_ReturnsReply checks reply text is returned.
func TestRunTurn_ReturnsReply(t *testing.T) {
	p := &fakeProvider{name: "fake", reply: "Hello user"}
	s := session.New()

	reply, err := RunTurn(
		context.Background(), p, s,
		"operate", "", "", "", "test",
		nil, nil, nil, llm.ModelInfo{},
	)
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	if reply != "Hello user" {
		t.Errorf("want reply 'Hello user', got %q", reply)
	}
}

// TestRunTurn_SystemPromptInRequest checks system prompt is included.
func TestRunTurn_SystemPromptInRequest(t *testing.T) {
	p := &fakeProvider{name: "fake", reply: "ok"}
	s := session.New()

	_, err := RunTurn(
		context.Background(), p, s,
		"operate", "", "", "", "hi",
		nil, nil, nil, llm.ModelInfo{},
	)
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}

	if len(p.calls) != 1 {
		t.Fatalf("want 1 call, got %d", len(p.calls))
	}

	if !strings.Contains(p.calls[0].System, "senior systems engineer") {
		t.Errorf("want system prompt containing operate content")
	}
}

// TestRunTurn_ScopeAppended checks scope is passed through.
func TestRunTurn_ScopeAppended(t *testing.T) {
	p := &fakeProvider{name: "fake", reply: "ok"}
	s := session.New()

	_, err := RunTurn(
		context.Background(), p, s,
		"operate", "MY SCOPE", "", "", "hi",
		nil, nil, nil, llm.ModelInfo{},
	)
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}

	if !strings.Contains(p.calls[0].System, "MY SCOPE") {
		t.Errorf("want system prompt containing scope")
	}
}

// TestREPL_QuitCommand_Exits checks /quit exits the REPL.
func TestREPL_QuitCommand_Exits(t *testing.T) {
	in := strings.NewReader("/quit\n")
	var out strings.Builder

	p := &fakeProvider{name: "fake"}
	r := &REPL{
		Provider: p,
		Session:  session.New(),
		Mode:     "operate",
		In:       in,
		Out:      &out,
	}

	err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !strings.Contains(out.String(), "shmorby>") {
		t.Errorf("want prompt in output")
	}
}

// TestREPL_ResetCommand_ClearsSession checks /reset clears the session.
func TestREPL_ResetCommand_ClearsSession(t *testing.T) {
	in := strings.NewReader("/reset\n/quit\n")
	var out strings.Builder

	p := &fakeProvider{name: "fake"}
	s := session.New()
	s.Append("user", "prior")

	r := &REPL{
		Provider: p,
		Session:  s,
		Mode:     "operate",
		In:       in,
		Out:      &out,
	}

	err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	msgs := s.Messages()
	if len(msgs) != 0 {
		t.Fatalf("want 0 messages after /reset, got %d", len(msgs))
	}
	if !strings.Contains(out.String(), "Session reset.") {
		t.Errorf("want 'Session reset.' in output")
	}
}

// TestREPL_ModelCommand_PrintsProvider checks /model prints provider and model.
func TestREPL_ModelCommand_PrintsProvider(t *testing.T) {
	in := strings.NewReader("/model\n/quit\n")
	var out strings.Builder

	p := &fakeProvider{name: "fake"}
	r := &REPL{
		Provider: p,
		Session:  session.New(),
		Mode:     "operate",
		Model:    "llama3",
		In:       in,
		Out:      &out,
	}

	err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !strings.Contains(out.String(), "fake (llama3)") {
		t.Errorf("want 'fake (llama3)' in output, got:\n%s", out.String())
	}
}

// TestREPL_AgentCommand_PrintsMode checks /agent prints current mode.
func TestREPL_AgentCommand_PrintsMode(t *testing.T) {
	in := strings.NewReader("/agent\n/quit\n")
	var out strings.Builder

	p := &fakeProvider{name: "fake"}
	r := &REPL{
		Provider: p,
		Session:  session.New(),
		Mode:     "operate",
		In:       in,
		Out:      &out,
	}

	err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !strings.Contains(out.String(), "operate") {
		t.Errorf("want 'operate' in output")
	}
}

// TestREPL_ScopeCommand_PrintsInfo checks /scope prints paths and size.
func TestREPL_ScopeCommand_PrintsInfo(t *testing.T) {
	in := strings.NewReader("/scope\n/quit\n")
	var out strings.Builder

	p := &fakeProvider{name: "fake"}
	r := &REPL{
		Provider: p,
		Session:  session.New(),
		Mode:     "operate",
		In:       in,
		Out:      &out,
		ScopeInfo: ScopeInfo{
			PrimaryPath:  "/path/to/SCOPE.md",
			Instructions: []string{"/path/to/inst.md"},
			TotalBytes:   1234,
		},
	}

	err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "1234 bytes") {
		t.Errorf("want '1234 bytes' in output, got:\n%s", output)
	}
	if !strings.Contains(output, "/path/to/SCOPE.md") {
		t.Errorf("want primary path in output, got:\n%s", output)
	}
	if !strings.Contains(output, "/path/to/inst.md") {
		t.Errorf("want instruction path in output, got:\n%s", output)
	}
}

// TestREPL_HelpCommand_PrintsCommands checks /help prints command list.
func TestREPL_HelpCommand_PrintsCommands(t *testing.T) {
	in := strings.NewReader("/help\n/quit\n")
	var out strings.Builder

	p := &fakeProvider{name: "fake"}
	r := &REPL{
		Provider: p,
		Session:  session.New(),
		Mode:     "operate",
		In:       in,
		Out:      &out,
	}

	err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "/quit") {
		t.Errorf("want /quit in help output, got:\n%s", output)
	}
	if !strings.Contains(output, "/help") {
		t.Errorf("want /help in help output, got:\n%s", output)
	}
	if !strings.Contains(output, "/scope") {
		t.Errorf("want /scope in help output, got:\n%s", output)
	}
}

// TestREPL_AgentDiagnose_SwitchesMode checks /agent diagnose changes mode.
func TestREPL_AgentDiagnose_SwitchesMode(t *testing.T) {
	in := strings.NewReader("/agent diagnose\n/agent\n/quit\n")
	var out strings.Builder

	p := &fakeProvider{name: "fake"}
	r := &REPL{
		Provider: p,
		Session:  session.New(),
		Mode:     "operate",
		In:       in,
		Out:      &out,
	}

	err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !strings.Contains(out.String(), "Switched to diagnose mode.") {
		t.Errorf("want switch message in output")
	}
	if !strings.Contains(out.String(), "diagnose") {
		t.Errorf("want 'diagnose' in output")
	}
}

// TestREPL_DiagnoseMode_SendsDiagnoseSystemPrompt checks diagnose mode sends
// diagnose system prompt on chat turn.
func TestREPL_DiagnoseMode_SendsDiagnoseSystemPrompt(t *testing.T) {
	in := strings.NewReader("/agent diagnose\nhello\n/quit\n")
	var out strings.Builder

	p := &fakeProvider{name: "fake", reply: "ok"}
	r := &REPL{
		Provider: p,
		Session:  session.New(),
		Mode:     "operate",
		In:       in,
		Out:      &out,
	}

	err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(p.calls) != 1 {
		t.Fatalf("want 1 provider call, got %d", len(p.calls))
	}

	if !strings.Contains(p.calls[0].System, "inspection and analysis") {
		t.Errorf("want diagnose system prompt, got:\n%s", p.calls[0].System)
	}
}

// TestREPL_ChatTurn_SendsToProvider checks normal input routes to provider.
func TestREPL_ChatTurn_SendsToProvider(t *testing.T) {
	in := strings.NewReader("hello\n/quit\n")
	var out strings.Builder

	p := &fakeProvider{name: "fake", reply: "world"}
	r := &REPL{
		Provider: p,
		Session:  session.New(),
		Mode:     "operate",
		In:       in,
		Out:      &out,
	}

	err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !strings.Contains(out.String(), "world") {
		t.Errorf("want 'world' in output")
	}
	if len(p.calls) != 1 {
		t.Fatalf("want 1 provider call, got %d", len(p.calls))
	}
}

// TestREPL_EmptyLine_Continues checks empty lines just re-prompt.
func TestREPL_EmptyLine_Continues(t *testing.T) {
	in := strings.NewReader("\n/quit\n")
	var out strings.Builder

	p := &fakeProvider{name: "fake"}
	r := &REPL{
		Provider: p,
		Session:  session.New(),
		Mode:     "operate",
		In:       in,
		Out:      &out,
	}

	err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Should have two prompts but zero provider calls.
	if len(p.calls) != 0 {
		t.Errorf("want 0 calls, got %d", len(p.calls))
	}
}

// TestREPL_EOF_Exits checks EOF on stdin returns nil.
func TestREPL_EOF_Exits(t *testing.T) {
	in := strings.NewReader("")
	var out strings.Builder

	p := &fakeProvider{name: "fake"}
	r := &REPL{
		Provider: p,
		Session:  session.New(),
		Mode:     "operate",
		In:       in,
		Out:      &out,
	}

	err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
}

// TestREPL_LLMError_PrintsAndContinues checks errors don't crash REPL.
func TestREPL_LLMError_PrintsAndContinues(t *testing.T) {
	p := &errorProvider{name: "fake"}
	in := strings.NewReader("hello\n/quit\n")
	var out strings.Builder

	r := &REPL{
		Provider: p,
		Session:  session.New(),
		Mode:     "operate",
		In:       in,
		Out:      &out,
	}

	err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !strings.Contains(out.String(), "Error:") {
		t.Errorf("want error printed in output")
	}
}

// errorProvider always returns an error.
type errorProvider struct{ name string }

// Returns the provider name.
func (e *errorProvider) Name() string { return e.name }

// Always returns an error.
func (e *errorProvider) Chat(
	ctx context.Context, req llm.ChatRequest,
) (llm.ChatResponse, error) {
	return llm.ChatResponse{}, fmt.Errorf("simulated failure")
}

// Streams a chat response (not implemented in test double).
func (e *errorProvider) ChatStream(
	_ context.Context, _ llm.ChatRequest,
) (<-chan llm.StreamEvent, error) {
	return nil, fmt.Errorf("fake: streaming not yet supported")
}

// Returns model info (not implemented in test double).
func (e *errorProvider) ModelInfo(
	_ context.Context, _ string,
) (llm.ModelInfo, error) {
	return llm.ModelInfo{}, nil
}

// fakeStepProvider returns a sequence of pre-defined responses.
type fakeStepProvider struct {
	name    string
	steps   []llm.ChatResponse
	callIdx int
	calls   []llm.ChatRequest
}

// Returns the provider name.
func (f *fakeStepProvider) Name() string { return f.name }

// Records the request and returns the next step.
func (f *fakeStepProvider) Chat(
	ctx context.Context, req llm.ChatRequest,
) (llm.ChatResponse, error) {
	f.calls = append(f.calls, req)
	if f.callIdx >= len(f.steps) {
		return llm.ChatResponse{},
			fmt.Errorf("unexpected call %d, only %d steps",
				f.callIdx, len(f.steps))
	}
	resp := f.steps[f.callIdx]
	f.callIdx++

	return resp, nil
}

// Streams a chat response (not implemented in test double).
func (f *fakeStepProvider) ChatStream(
	_ context.Context, _ llm.ChatRequest,
) (<-chan llm.StreamEvent, error) {
	return nil, fmt.Errorf("fake: streaming not yet supported")
}

// Returns model info (not implemented in test double).
func (f *fakeStepProvider) ModelInfo(
	_ context.Context, _ string,
) (llm.ModelInfo, error) {
	return llm.ModelInfo{}, nil
}

// fakeTool is a test double for tools.Tool.
type fakeTool struct {
	name   string
	result string
	err    error
	perm   string
}

// Returns the tool name.
func (f *fakeTool) Name() string { return f.name }

// Returns a static description.
func (f *fakeTool) Description() string { return "fake tool for testing" }

// Returns an empty object schema.
func (f *fakeTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}

// PermLevel returns the configured permission level.
func (f *fakeTool) PermLevel() string {
	if f.perm != "" {
		return f.perm
	}
	return "allow"
}

// Returns the canned result or error.
func (f *fakeTool) Run(
	_ context.Context, _ json.RawMessage,
) (string, error) {
	return f.result, f.err
}

// TestRunTurnWithTools_ToolCallThenResult checks tool call → result → text.
func TestRunTurnWithTools_ToolCallThenResult(t *testing.T) {
	toolResp := llm.ChatResponse{
		Message: llm.Message{Role: "assistant", Content: "Running..."},
		ToolCalls: []llm.ToolCall{
			{ID: "call_1", Name: "shell", Args: `{"command":"echo hi"}`},
		},
	}
	textResp := llm.ChatResponse{
		Message: llm.Message{Role: "assistant", Content: "Output: hi"},
	}
	p := &fakeStepProvider{
		name:  "fake",
		steps: []llm.ChatResponse{toolResp, textResp},
	}
	s := session.New()

	reg := tools.NewRegistry()
	reg.Register(&fakeTool{name: "shell", result: "hi"})

	reply, err := RunTurnWithTools(
		context.Background(), p, s,
		"operate", "", "", "m", "list files",
		reg, 5, true,
		nil, nil, nil, llm.ModelInfo{},
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("RunTurnWithTools: %v", err)
	}
	if reply != "Output: hi" {
		t.Errorf("want reply %q, got %q", "Output: hi", reply)
	}

	msgs := s.Messages()
	if len(msgs) != 4 {
		t.Fatalf("want 4 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "list files" {
		t.Errorf("msg[0]: want user/list files, got %s/%s",
			msgs[0].Role, msgs[0].Content)
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != "Running..." {
		t.Errorf("msg[1]: want assistant/Running..., got %s/%s",
			msgs[1].Role, msgs[1].Content)
	}
	if len(msgs[1].ToolCalls) != 1 {
		t.Errorf("msg[1]: want 1 ToolCall, got %d",
			len(msgs[1].ToolCalls))
	}
	if msgs[2].Role != "tool" || msgs[2].Content != "hi" {
		t.Errorf("msg[2]: want tool/hi, got %s/%s",
			msgs[2].Role, msgs[2].Content)
	}
	if msgs[2].ToolName != "shell" || msgs[2].ToolCallID != "call_1" {
		t.Errorf("msg[2]: want ToolName=shell ToolCallID=call_1, got %s/%s",
			msgs[2].ToolName, msgs[2].ToolCallID)
	}
	if msgs[3].Role != "assistant" || msgs[3].Content != "Output: hi" {
		t.Errorf("msg[3]: want assistant/Output: hi, got %s/%s",
			msgs[3].Role, msgs[3].Content)
	}
}

// TestRunTurnWithTools_MaxIterations checks iteration limit stops loop
// and triggers a final summary LLM call without tools.
func TestRunTurnWithTools_MaxIterations(t *testing.T) {
	toolResp := llm.ChatResponse{
		Message: llm.Message{Role: "assistant", Content: "Running..."},
		ToolCalls: []llm.ToolCall{
			{ID: "call_1", Name: "shell", Args: `{}`},
		},
	}
	summaryResp := llm.ChatResponse{
		Message: llm.Message{Role: "assistant",
			Content: "Summary: completed 2 steps."},
	}
	p := &fakeStepProvider{
		name:  "fake",
		steps: []llm.ChatResponse{toolResp, toolResp, summaryResp},
	}
	s := session.New()

	reg := tools.NewRegistry()
	reg.Register(&fakeTool{name: "shell", result: "ok"})

	reply, err := RunTurnWithTools(
		context.Background(), p, s,
		"operate", "", "", "m", "do it",
		reg, 2, true,
		nil, nil, nil, llm.ModelInfo{},
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("RunTurnWithTools: %v", err)
	}
	if reply != "Summary: completed 2 steps." {
		t.Errorf("want summary response, got %q", reply)
	}

	msgs := s.Messages()
	if len(msgs) != 7 {
		t.Fatalf("want 7 messages, got %d", len(msgs))
	}
	// user + assistant*2 + tool*2 + user_summary + assistant_summary
	if msgs[0].Role != "user" {
		t.Errorf("msg[0]: want user, got %s", msgs[0].Role)
	}
	if msgs[1].Role != "assistant" {
		t.Errorf("msg[1]: want assistant, got %s", msgs[1].Role)
	}
	if msgs[2].Role != "tool" {
		t.Errorf("msg[2]: want tool, got %s", msgs[2].Role)
	}
	if msgs[3].Role != "assistant" {
		t.Errorf("msg[3]: want assistant, got %s", msgs[3].Role)
	}
	if msgs[4].Role != "tool" {
		t.Errorf("msg[4]: want tool, got %s", msgs[4].Role)
	}
	if msgs[5].Role != "user" {
		t.Errorf("msg[5]: want user (summary prompt), got %s",
			msgs[5].Role)
	}
	if msgs[5].Content != MaxStepsPrompt {
		t.Errorf("msg[5]: want MaxStepsPrompt content, got %q",
			msgs[5].Content)
	}
	if msgs[6].Role != "assistant" {
		t.Errorf("msg[6]: want assistant (summary), got %s",
			msgs[6].Role)
	}
	if msgs[6].Content != "Summary: completed 2 steps." {
		t.Errorf("msg[6]: want summary content, got %q",
			msgs[6].Content)
	}

	// Final summary call should have no tools and contain the embedded
	// template in the last user message.
	if len(p.calls) != 3 {
		t.Fatalf("want 3 provider calls, got %d", len(p.calls))
	}
	if len(p.calls[2].Tools) != 0 {
		t.Errorf("summary call: want 0 tools, got %d",
			len(p.calls[2].Tools))
	}
	lastUser := p.calls[2].Messages[len(p.calls[2].Messages)-1]
	if lastUser.Role != "user" {
		t.Errorf("summary last msg: want user role, got %s",
			lastUser.Role)
	}
	if lastUser.Content != MaxStepsPrompt {
		t.Errorf("summary last msg: want MaxStepsPrompt, got %q",
			lastUser.Content)
	}
}

// TestRunTurnWithTools_UnknownToolError checks unknown tool error is fed back.
func TestRunTurnWithTools_UnknownToolError(t *testing.T) {
	toolResp := llm.ChatResponse{
		Message: llm.Message{Role: "assistant", Content: "Running..."},
		ToolCalls: []llm.ToolCall{
			{ID: "call_1", Name: "nonexistent", Args: `{}`},
		},
	}
	textResp := llm.ChatResponse{
		Message: llm.Message{Role: "assistant", Content: "Saw error, continuing"},
	}
	p := &fakeStepProvider{
		name:  "fake",
		steps: []llm.ChatResponse{toolResp, textResp},
	}
	s := session.New()

	reg := tools.NewRegistry()

	reply, err := RunTurnWithTools(
		context.Background(), p, s,
		"operate", "", "", "m", "test",
		reg, 5, true,
		nil, nil, nil, llm.ModelInfo{},
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("RunTurnWithTools: %v", err)
	}
	if reply != "Saw error, continuing" {
		t.Errorf("want reply %q, got %q",
			"Saw error, continuing", reply)
	}

	msgs := s.Messages()
	if len(msgs) != 4 {
		t.Fatalf("want 4 messages, got %d", len(msgs))
	}
	if msgs[2].Role != "tool" {
		t.Errorf("msg[2]: want tool role, got %s", msgs[2].Role)
	}
	if !strings.Contains(msgs[2].Content, "tool not found") {
		t.Errorf("msg[2]: want error about tool not found, got %q",
			msgs[2].Content)
	}
	if msgs[2].ToolName != "nonexistent" {
		t.Errorf("msg[2]: want ToolName=nonexistent, got %s",
			msgs[2].ToolName)
	}
}

// TestRunTurnWithTools_SecondChat_IncludesAssistantToolCalls checks that
// tool_calls from the first iteration are present in the second request.
func TestRunTurnWithTools_SecondChat_IncludesAssistantToolCalls(t *testing.T) {
	toolResp := llm.ChatResponse{
		Message: llm.Message{Role: "assistant", Content: "Running..."},
		ToolCalls: []llm.ToolCall{
			{ID: "call_1", Name: "shell", Args: `{}`},
		},
	}
	textResp := llm.ChatResponse{
		Message: llm.Message{Role: "assistant", Content: "Done"},
	}
	p := &fakeStepProvider{
		name:  "fake",
		steps: []llm.ChatResponse{toolResp, textResp},
	}
	s := session.New()

	reg := tools.NewRegistry()
	reg.Register(&fakeTool{name: "shell", result: "ok"})

	_, err := RunTurnWithTools(
		context.Background(), p, s,
		"operate", "", "", "m", "test",
		reg, 5, true,
		nil, nil, nil, llm.ModelInfo{},
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("RunTurnWithTools: %v", err)
	}

	if len(p.calls) < 2 {
		t.Fatalf("want 2+ provider calls, got %d", len(p.calls))
	}

	// Second request's second message should be the assistant with
	// tool_calls from iteration 0.
	req2 := p.calls[1]
	if len(req2.Messages) < 2 {
		t.Fatalf("want 2+ messages in second request, got %d",
			len(req2.Messages))
	}

	msg1 := req2.Messages[1]
	if msg1.Role != "assistant" {
		t.Fatalf("want assistant role, got %q", msg1.Role)
	}
	if len(msg1.ToolCalls) == 0 {
		t.Fatal("want non-empty ToolCalls on assistant message in " +
			"second request")
	}
	if msg1.ToolCalls[0].ID != "call_1" {
		t.Errorf("want ToolCall ID 'call_1', got %q",
			msg1.ToolCalls[0].ID)
	}
	if msg1.ToolCalls[0].Name != "shell" {
		t.Errorf("want ToolCall Name 'shell', got %q",
			msg1.ToolCalls[0].Name)
	}
}

// TestRunTurnWithTools_PartialOutputOnError checks partial stdout is
// preserved when a tool returns both output and an error.
func TestRunTurnWithTools_PartialOutputOnError(t *testing.T) {
	toolResp := llm.ChatResponse{
		Message: llm.Message{Role: "assistant", Content: "Running..."},
		ToolCalls: []llm.ToolCall{
			{ID: "call_1", Name: "partial_tool", Args: `{}`},
		},
	}
	textResp := llm.ChatResponse{
		Message: llm.Message{Role: "assistant", Content: "Done"},
	}
	p := &fakeStepProvider{
		name:  "fake",
		steps: []llm.ChatResponse{toolResp, textResp},
	}
	s := session.New()

	reg := tools.NewRegistry()
	reg.Register(&fakeTool{
		name:   "partial_tool",
		result: "partial output",
		err:    fmt.Errorf("timeout"),
	})

	reply, err := RunTurnWithTools(
		context.Background(), p, s,
		"operate", "", "", "m", "test",
		reg, 5, true,
		nil, nil, nil, llm.ModelInfo{},
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("RunTurnWithTools: %v", err)
	}
	if reply != "Done" {
		t.Errorf("want reply 'Done', got %q", reply)
	}

	msgs := s.Messages()
	if len(msgs) != 4 {
		t.Fatalf("want 4 messages, got %d", len(msgs))
	}
	// Tool result should contain partial output followed by error.
	toolMsg := msgs[2]
	if !strings.Contains(toolMsg.Content, "partial output") {
		t.Errorf("want partial output preserved, got %q",
			toolMsg.Content)
	}
	if !strings.Contains(toolMsg.Content, "error: timeout") {
		t.Errorf("want error in result, got %q", toolMsg.Content)
	}
}

// TestRunTurnWithTools_ShellDisabled verifies no tool definitions are sent
// when shellEnabled is false.
func TestRunTurnWithTools_ShellDisabled(t *testing.T) {
	textResp := llm.ChatResponse{
		Message: llm.Message{Role: "assistant", Content: "Hello"},
	}
	p := &fakeStepProvider{
		name:  "fake",
		steps: []llm.ChatResponse{textResp},
	}
	s := session.New()

	reg := tools.NewRegistry()
	reg.Register(&fakeTool{name: "shell", result: "ok"})

	_, err := RunTurnWithTools(
		context.Background(), p, s,
		"operate", "", "", "m", "hi",
		reg, 5, false,
		nil, nil, nil, llm.ModelInfo{},
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("RunTurnWithTools: %v", err)
	}

	if len(p.calls) != 1 {
		t.Fatalf("want 1 provider call, got %d", len(p.calls))
	}

	if len(p.calls[0].Tools) != 0 {
		t.Errorf("want 0 tool definitions when shell disabled, got %d",
			len(p.calls[0].Tools))
	}
}

// TestREPL_ChatTurn_WithToolsPath checks REPL with Registry routes through
// RunTurnWithTools and prints tool-backed replies.
func TestREPL_ChatTurn_WithToolsPath(t *testing.T) {
	toolResp := llm.ChatResponse{
		Message: llm.Message{Role: "assistant", Content: "Running..."},
		ToolCalls: []llm.ToolCall{
			{ID: "call_1", Name: "shell", Args: `{}`},
		},
	}
	textResp := llm.ChatResponse{
		Message: llm.Message{Role: "assistant", Content: "Output: ok"},
	}
	p := &fakeStepProvider{
		name:  "fake",
		steps: []llm.ChatResponse{toolResp, textResp},
	}
	s := session.New()

	reg := tools.NewRegistry()
	reg.Register(&fakeTool{name: "shell", result: "ok"})

	in := strings.NewReader("run command\n/quit\n")
	var out strings.Builder

	r := &REPL{
		Provider:     p,
		Session:      s,
		Mode:         "operate",
		Model:        "m",
		In:           in,
		Out:          &out,
		Registry:     reg,
		MaxToolIter:  5,
		ShellEnabled: true,
	}

	err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !strings.Contains(out.String(), "Output: ok") {
		t.Errorf("want tool-backed reply in output, got:\n%s",
			out.String())
	}
	if len(p.calls) != 2 {
		t.Errorf("want 2 provider calls (tool call + text), got %d",
			len(p.calls))
	}
}

// TestREPL_PermissionPrompt_Allow checks toolPermissionFunc returns
// PermAllow for "y" input.
func TestREPL_PermissionPrompt_Allow(t *testing.T) {
	r := &REPL{
		In:      strings.NewReader("y\n"),
		Out:     &strings.Builder{},
		scanner: bufio.NewScanner(strings.NewReader("y\n")),
	}

	result := r.toolPermissionFunc("shell", "reboot", "restart")
	if result != PermAllow {
		t.Errorf("want PermAllow, got %v", result)
	}
}

// TestREPL_PermissionPrompt_Deny checks toolPermissionFunc returns
// PermDeny for "n" input.
func TestREPL_PermissionPrompt_Deny(t *testing.T) {
	r := &REPL{
		In:      strings.NewReader("n\n"),
		Out:     &strings.Builder{},
		scanner: bufio.NewScanner(strings.NewReader("n\n")),
	}

	result := r.toolPermissionFunc("shell", "reboot", "restart")
	if result != PermDeny {
		t.Errorf("want PermDeny, got %v", result)
	}
}

// TestREPL_PermissionPrompt_AllowAll checks toolPermissionFunc returns
// PermAllowAll for "a" input.
func TestREPL_PermissionPrompt_AllowAll(t *testing.T) {
	r := &REPL{
		In:      strings.NewReader("a\n"),
		Out:     &strings.Builder{},
		scanner: bufio.NewScanner(strings.NewReader("a\n")),
	}

	result := r.toolPermissionFunc("shell", "reboot", "restart")
	if result != PermAllowAll {
		t.Errorf("want PermAllowAll, got %v", result)
	}
}

// TestRunTurnWithTools_DiagnoseBlocksMutatingShell checks that
// diagnose mode blocks a mutating shell command.
func TestRunTurnWithTools_DiagnoseBlocksMutatingShell(t *testing.T) {
	toolResp := llm.ChatResponse{
		Message: llm.Message{Role: "assistant", Content: "Running..."},
		ToolCalls: []llm.ToolCall{
			{ID: "call_1", Name: "shell",
				Args: `{"command":"rm -rf /tmp/x"}`},
		},
	}
	textResp := llm.ChatResponse{
		Message: llm.Message{Role: "assistant", Content: "Blocked"},
	}
	p := &fakeStepProvider{
		name:  "fake",
		steps: []llm.ChatResponse{toolResp, textResp},
	}
	s := session.New()

	reg := tools.NewRegistry()
	reg.Register(&fakeTool{name: "shell", result: "would-run"})

	reply, err := RunTurnWithTools(
		context.Background(), p, s,
		"diagnose", "", "", "m", "delete files",
		reg, 5, true,
		nil, nil, nil, llm.ModelInfo{},
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("RunTurnWithTools: %v", err)
	}
	if reply != "Blocked" {
		t.Errorf("want reply %q, got %q", "Blocked", reply)
	}

	msgs := s.Messages()
	if len(msgs) != 4 {
		t.Fatalf("want 4 messages, got %d", len(msgs))
	}
	// Tool result message should contain the diagnose error.
	toolMsg := msgs[2]
	if !strings.Contains(toolMsg.Content, "diagnose:") {
		t.Errorf("want 'diagnose:' in tool result, got %q",
			toolMsg.Content)
	}
	if toolMsg.Content == "would-run" {
		t.Errorf("guard bypassed: tool executed unguarded")
	}
}

// TestRunTurnWithTools_OperateAllowsMutatingShell checks that operate
// mode permits a mutating shell command.
func TestRunTurnWithTools_OperateAllowsMutatingShell(t *testing.T) {
	toolResp := llm.ChatResponse{
		Message: llm.Message{Role: "assistant", Content: "Running..."},
		ToolCalls: []llm.ToolCall{
			{ID: "call_1", Name: "shell",
				Args: `{"command":"rm -rf /tmp/x"}`},
		},
	}
	textResp := llm.ChatResponse{
		Message: llm.Message{Role: "assistant", Content: "Executed"},
	}
	p := &fakeStepProvider{
		name:  "fake",
		steps: []llm.ChatResponse{toolResp, textResp},
	}
	s := session.New()

	reg := tools.NewRegistry()
	reg.Register(&fakeTool{name: "shell", result: "removed"})

	reply, err := RunTurnWithTools(
		context.Background(), p, s,
		"operate", "", "", "m", "delete files",
		reg, 5, true,
		nil, nil, nil, llm.ModelInfo{},
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("RunTurnWithTools: %v", err)
	}
	if reply != "Executed" {
		t.Errorf("want reply %q, got %q", "Executed", reply)
	}

	msgs := s.Messages()
	if len(msgs) != 4 {
		t.Fatalf("want 4 messages, got %d", len(msgs))
	}
	toolMsg := msgs[2]
	if !strings.Contains(toolMsg.Content, "removed") {
		t.Errorf("want 'removed' in tool result, got %q",
			toolMsg.Content)
	}
}

// TestRunTurnWithTools_ShellDisabledStillAdvertisesNonShell checks that
// non-shell tools (ssh) are advertised even when shell is disabled.
func TestRunTurnWithTools_ShellDisabledStillAdvertisesNonShell(t *testing.T) {
	textResp := llm.ChatResponse{
		Message: llm.Message{Role: "assistant", Content: "Hello"},
	}
	p := &fakeStepProvider{
		name:  "fake",
		steps: []llm.ChatResponse{textResp},
	}
	s := session.New()

	reg := tools.NewRegistry()
	reg.Register(&fakeTool{name: "shell", result: "ok"})
	reg.Register(&fakeTool{name: "ssh", result: "ok"})

	_, err := RunTurnWithTools(
		context.Background(), p, s,
		"operate", "", "", "m", "hi",
		reg, 5, false,
		nil, nil, nil, llm.ModelInfo{},
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("RunTurnWithTools: %v", err)
	}

	if len(p.calls) != 1 {
		t.Fatalf("want 1 provider call, got %d", len(p.calls))
	}

	// Should advertise ssh but not shell.
	foundShell := false
	foundSSH := false
	for _, td := range p.calls[0].Tools {
		if td.Name == "shell" {
			foundShell = true
		}
		if td.Name == "ssh" {
			foundSSH = true
		}
	}
	if foundShell {
		t.Errorf("shell tool should not be advertised when disabled")
	}
	if !foundSSH {
		t.Errorf("ssh tool should be advertised even when shell disabled")
	}
}

// TestRunTurnWithTools_DiagnoseBadArgs_Blocked checks that invalid or
// empty shell args in diagnose mode are rejected (P08-02).
func TestRunTurnWithTools_DiagnoseBadArgs_Blocked(t *testing.T) {
	toolResp := llm.ChatResponse{
		Message: llm.Message{Role: "assistant", Content: "Running..."},
		ToolCalls: []llm.ToolCall{
			{ID: "call_1", Name: "shell",
				Args: `{"bad": "json"}`},
		},
	}
	textResp := llm.ChatResponse{
		Message: llm.Message{Role: "assistant", Content: "Blocked"},
	}
	p := &fakeStepProvider{
		name:  "fake",
		steps: []llm.ChatResponse{toolResp, textResp},
	}
	s := session.New()

	reg := tools.NewRegistry()
	reg.Register(&fakeTool{name: "shell", result: "would-run"})

	reply, err := RunTurnWithTools(
		context.Background(), p, s,
		"diagnose", "", "", "m", "bad args",
		reg, 5, true,
		nil, nil, nil, llm.ModelInfo{},
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("RunTurnWithTools: %v", err)
	}
	if reply != "Blocked" {
		t.Errorf("want reply %q, got %q", "Blocked", reply)
	}

	msgs := s.Messages()
	if len(msgs) != 4 {
		t.Fatalf("want 4 messages, got %d", len(msgs))
	}
	toolMsg := msgs[2]
	if !strings.Contains(toolMsg.Content,
		"invalid or empty command") {
		t.Errorf("want rejection in tool result, got %q",
			toolMsg.Content)
	}
	if toolMsg.Content == "would-run" {
		t.Errorf("guard bypassed: tool executed with bad args")
	}
}

// TestFilterDiagnoseSchemas_AllAllowed checks that diagnose mode
// allows shell, ssh, sudo, aws.
func TestFilterDiagnoseSchemas_AllAllowed(t *testing.T) {
	schemas := []tools.ToolSchema{
		{Name: "shell"},
		{Name: "ssh"},
		{Name: "sudo"},
		{Name: "aws"},
	}
	filtered := filterDiagnoseSchemas(schemas)
	if len(filtered) != 4 {
		t.Fatalf("want 4 schemas, got %d", len(filtered))
	}
}

// TestFilterDiagnoseSchemas_UnknownBlocked checks that unknown tools
// are filtered out in diagnose mode.
func TestFilterDiagnoseSchemas_UnknownBlocked(t *testing.T) {
	schemas := []tools.ToolSchema{
		{Name: "shell"},
		{Name: "unknown_tool"},
		{Name: "kubectl"},
	}
	filtered := filterDiagnoseSchemas(schemas)
	if len(filtered) != 1 {
		t.Fatalf("want 1 schema (shell only), got %d", len(filtered))
	}
	if filtered[0].Name != "shell" {
		t.Errorf("want 'shell', got %q", filtered[0].Name)
	}
}

// TestRunTurnWithTools_ClampMaxIterations checks maxIterations <= 0 is
// clamped to 1, still making at least one LLM call.
func TestRunTurnWithTools_ClampMaxIterations(t *testing.T) {
	textResp := llm.ChatResponse{
		Message: llm.Message{Role: "assistant", Content: "hello"},
	}
	p := &fakeStepProvider{
		name:  "fake",
		steps: []llm.ChatResponse{textResp},
	}
	s := session.New()

	reg := tools.NewRegistry()
	reg.Register(&fakeTool{name: "shell", result: "ok"})

	reply, err := RunTurnWithTools(
		context.Background(), p, s,
		"operate", "", "", "m", "hi",
		reg, 0, true,
		nil, nil, nil, llm.ModelInfo{},
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("RunTurnWithTools: %v", err)
	}
	if reply != "hello" {
		t.Errorf("want reply 'hello', got %q", reply)
	}
	if len(p.calls) != 1 {
		t.Errorf("want 1 provider call (clamped to 1), got %d",
			len(p.calls))
	}
}

// TestRunTurnWithTools_MaxIterationsSummaryFailure checks that when the
// final summary Chat fails, a generic iteration-limit message is returned.
func TestRunTurnWithTools_MaxIterationsSummaryFailure(t *testing.T) {
	toolResp := llm.ChatResponse{
		Message: llm.Message{Role: "assistant", Content: "Running..."},
		ToolCalls: []llm.ToolCall{
			{ID: "call_1", Name: "shell", Args: `{}`},
		},
	}
	// Only provide 2 steps; the summary call (3rd) will error.
	p := &fakeStepProvider{
		name:  "fake",
		steps: []llm.ChatResponse{toolResp, toolResp},
	}
	s := session.New()

	reg := tools.NewRegistry()
	reg.Register(&fakeTool{name: "shell", result: "ok"})

	reply, err := RunTurnWithTools(
		context.Background(), p, s,
		"operate", "", "", "m", "do it",
		reg, 2, true,
		nil, nil, nil, llm.ModelInfo{},
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("RunTurnWithTools: %v", err)
	}
	if !strings.Contains(reply, "iteration limit") {
		t.Errorf("want fallback iteration limit message, got %q",
			reply)
	}

	msgs := s.Messages()
	if len(msgs) != 7 {
		t.Fatalf("want 7 messages, got %d", len(msgs))
	}
	// Last assistant message should be the fallback.
	if msgs[6].Role != "assistant" {
		t.Errorf("msg[6]: want assistant, got %s", msgs[6].Role)
	}
	if !strings.Contains(msgs[6].Content, "iteration limit") {
		t.Errorf("msg[6]: want iteration limit content, got %q",
			msgs[6].Content)
	}
}

// TestRunTurnWithTools_PermissionDenyBlocks checks tool-level "deny"
// blocks execution even with nil permFunc.
func TestRunTurnWithTools_PermissionDenyBlocks(t *testing.T) {
	toolResp := llm.ChatResponse{
		Message: llm.Message{Role: "assistant", Content: "Running..."},
		ToolCalls: []llm.ToolCall{
			{ID: "call_1", Name: "shell", Args: `{"command":"rm -rf /"}`},
		},
	}
	textResp := llm.ChatResponse{
		Message: llm.Message{Role: "assistant", Content: "Blocked"},
	}
	p := &fakeStepProvider{
		name:  "fake",
		steps: []llm.ChatResponse{toolResp, textResp},
	}
	s := session.New()

	reg := tools.NewRegistry()
	reg.Register(&fakeTool{
		name: "shell", result: "should-not-run", perm: "deny",
	})

	reply, err := RunTurnWithTools(
		context.Background(), p, s,
		"operate", "", "", "m", "delete files",
		reg, 5, true,
		nil, nil, nil, llm.ModelInfo{},
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("RunTurnWithTools: %v", err)
	}
	if reply != "Blocked" {
		t.Errorf("want reply %q, got %q", "Blocked", reply)
	}

	msgs := s.Messages()
	if len(msgs) != 4 {
		t.Fatalf("want 4 messages, got %d", len(msgs))
	}
	toolMsg := msgs[2]
	if !strings.Contains(toolMsg.Content, "permission denied") {
		t.Errorf("want 'permission denied' in tool result, got %q",
			toolMsg.Content)
	}
	if toolMsg.Content == "should-not-run" {
		t.Errorf("tool executed despite deny perm level")
	}
}

// TestRunTurnWithTools_PermissionAsk_DefaultAllow checks "ask" with
// nil permFunc defaults to allow (v1 backward compatibility).
func TestRunTurnWithTools_PermissionAsk_DefaultAllow(t *testing.T) {
	toolResp := llm.ChatResponse{
		Message: llm.Message{Role: "assistant", Content: "Running..."},
		ToolCalls: []llm.ToolCall{
			{ID: "call_1", Name: "shell", Args: `{"command":"echo hi"}`},
		},
	}
	textResp := llm.ChatResponse{
		Message: llm.Message{Role: "assistant", Content: "Output: hi"},
	}
	p := &fakeStepProvider{
		name:  "fake",
		steps: []llm.ChatResponse{toolResp, textResp},
	}
	s := session.New()

	reg := tools.NewRegistry()
	reg.Register(&fakeTool{
		name: "shell", result: "echo hi", perm: "ask",
	})

	reply, err := RunTurnWithTools(
		context.Background(), p, s,
		"operate", "", "", "m", "say hi",
		reg, 5, true,
		nil, nil, nil, llm.ModelInfo{},
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("RunTurnWithTools: %v", err)
	}
	if reply != "Output: hi" {
		t.Errorf("want reply %q, got %q", "Output: hi", reply)
	}

	msgs := s.Messages()
	if len(msgs) != 4 {
		t.Fatalf("want 4 messages, got %d", len(msgs))
	}
	if !strings.Contains(msgs[2].Content, "echo hi") {
		t.Errorf("want tool to have executed, got %q", msgs[2].Content)
	}
}

// TestRunTurnWithTools_PermissionAsk_DeniedByPermFunc checks permFunc
// returning PermDeny blocks execution for "ask" tools.
func TestRunTurnWithTools_PermissionAsk_DeniedByPermFunc(t *testing.T) {
	toolResp := llm.ChatResponse{
		Message: llm.Message{Role: "assistant", Content: "Running..."},
		ToolCalls: []llm.ToolCall{
			{ID: "call_1", Name: "shell", Args: `{"command":"reboot"}`},
		},
	}
	textResp := llm.ChatResponse{
		Message: llm.Message{Role: "assistant", Content: "Denied"},
	}
	p := &fakeStepProvider{
		name:  "fake",
		steps: []llm.ChatResponse{toolResp, textResp},
	}
	s := session.New()

	reg := tools.NewRegistry()
	reg.Register(&fakeTool{
		name: "shell", result: "should-not-run", perm: "ask",
	})

	permFunc := func(toolName, command, reason string) ToolPermissionResponse {
		return PermDeny
	}

	reply, err := RunTurnWithTools(
		context.Background(), p, s,
		"operate", "", "", "m", "reboot",
		reg, 5, true,
		nil, nil, nil, llm.ModelInfo{},
		nil,
		permFunc,
		nil,
	)
	if err != nil {
		t.Fatalf("RunTurnWithTools: %v", err)
	}
	if reply != "Denied" {
		t.Errorf("want reply %q, got %q", "Denied", reply)
	}

	msgs := s.Messages()
	if len(msgs) != 4 {
		t.Fatalf("want 4 messages, got %d", len(msgs))
	}
	toolMsg := msgs[2]
	if !strings.Contains(toolMsg.Content, "permission denied") {
		t.Errorf("want 'permission denied' in tool result, got %q",
			toolMsg.Content)
	}
	if toolMsg.Content == "should-not-run" {
		t.Errorf("tool executed despite permFunc deny")
	}
}

// fakeStreamProvider is a test double with configurable stream function.
type fakeStreamProvider struct {
	name     string
	streamFn func(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error)
	chatFn   func(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error)
	chatCall int
}

func (f *fakeStreamProvider) Name() string { return f.name }

func (f *fakeStreamProvider) Chat(
	ctx context.Context, req llm.ChatRequest,
) (llm.ChatResponse, error) {
	f.chatCall++
	if f.chatFn != nil {
		return f.chatFn(ctx, req)
	}
	return llm.ChatResponse{}, fmt.Errorf("fake: chat not implemented")
}

func (f *fakeStreamProvider) ChatStream(
	ctx context.Context, req llm.ChatRequest,
) (<-chan llm.StreamEvent, error) {
	if f.streamFn != nil {
		return f.streamFn(ctx, req)
	}
	return nil, fmt.Errorf("fake: stream not implemented")
}

func (f *fakeStreamProvider) ModelInfo(
	_ context.Context, _ string,
) (llm.ModelInfo, error) {
	return llm.ModelInfo{}, nil
}

// sleepyTool is a tool that delays execution for testing spinner visibility.
type sleepyTool struct {
	name   string
	result string
	err    error
	sleep  time.Duration
}

func (s *sleepyTool) Name() string        { return s.name }
func (s *sleepyTool) Description() string { return "sleepy tool for testing" }
func (s *sleepyTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (s *sleepyTool) PermLevel() string { return "allow" }
func (s *sleepyTool) Run(ctx context.Context, _ json.RawMessage) (string, error) {
	select {
	case <-time.After(s.sleep):
	case <-ctx.Done():
		return "", ctx.Err()
	}
	return s.result, s.err
}

// streamTextHelper creates a streamFn that emits the given deltas then done.
func streamTextHelper(deltas ...string) func(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	return func(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
		ch := make(chan llm.StreamEvent)
		go func() {
			for _, d := range deltas {
				ch <- llm.StreamEvent{Type: "text", Delta: d}
			}
			ch <- llm.StreamEvent{Type: "done"}
			close(ch)
		}()
		return ch, nil
	}
}

// streamToolCallHelper creates a streamFn that emits text, then a tool-call,
// then done.
func streamToolCallHelper(toolName, toolID, args string) func(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	return func(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
		ch := make(chan llm.StreamEvent)
		go func() {
			ch <- llm.StreamEvent{Type: "text", Delta: "Running " + toolName}
			ch <- llm.StreamEvent{
				Type:    "tool-call",
				ToolID:  toolID,
				Tool:    toolName,
				Content: args,
			}
			ch <- llm.StreamEvent{Type: "done"}
			close(ch)
		}()
		return ch, nil
	}
}

// TestRunTurnWithToolsStream_TextDeltas checks deltas passed to onDelta.
func TestRunTurnWithToolsStream_TextDeltas(t *testing.T) {
	var gotDeltas []string
	p := &fakeStreamProvider{
		name:     "fake",
		streamFn: streamTextHelper("Hello", " world", "!"),
	}
	sess := session.New()

	reply, err := RunTurnWithToolsStream(
		context.Background(), p, sess,
		"operate", "", "", "test-model", "hi",
		nil, 5, true, nil, nil, nil, llm.ModelInfo{},
		nil,
		func(delta string) { gotDeltas = append(gotDeltas, delta) },
		nil, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	wantDeltas := []string{"Hello", " world", "!"}
	if !stringsEqual(gotDeltas, wantDeltas) {
		t.Errorf("deltas = %v, want %v", gotDeltas, wantDeltas)
	}
	if reply != "Hello world!" {
		t.Errorf("reply = %q, want %q", reply, "Hello world!")
	}
}

func stringsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestRunTurnWithToolsStream_AccumulatesText checks final text = all deltas.
func TestRunTurnWithToolsStream_AccumulatesText(t *testing.T) {
	p := &fakeStreamProvider{
		name:     "fake",
		streamFn: streamTextHelper("one", " two", " three"),
	}
	sess := session.New()

	reply, err := RunTurnWithToolsStream(
		context.Background(), p, sess,
		"operate", "", "", "test-model", "hi",
		nil, 5, true, nil, nil, nil, llm.ModelInfo{},
		nil, nil, nil, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if reply != "one two three" {
		t.Errorf("reply = %q, want %q", reply, "one two three")
	}
}

// TestRunTurnWithToolsStream_ReasoningDeltas checks reasoning forwarded.
func TestRunTurnWithToolsStream_ReasoningDeltas(t *testing.T) {
	var gotDeltas []string
	p := &fakeStreamProvider{
		name: "fake",
		streamFn: func(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
			ch := make(chan llm.StreamEvent)
			go func() {
				ch <- llm.StreamEvent{Type: "reasoning", Delta: "thinking step 1"}
				ch <- llm.StreamEvent{Type: "text", Delta: "answer"}
				ch <- llm.StreamEvent{Type: "done"}
				close(ch)
			}()
			return ch, nil
		},
	}
	sess := session.New()

	reply, err := RunTurnWithToolsStream(
		context.Background(), p, sess,
		"operate", "", "", "test-model", "hi",
		nil, 5, true, nil, nil, nil, llm.ModelInfo{},
		nil,
		func(delta string) { gotDeltas = append(gotDeltas, delta) },
		nil, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if reply != "thinking step 1answer" {
		t.Errorf("reply = %q, want combined text", reply)
	}
	if len(gotDeltas) != 2 {
		t.Errorf("want 2 deltas, got %d", len(gotDeltas))
	}
}

// TestRunTurnWithToolsStream_ToolCall_Executes checks tool runs.
func TestRunTurnWithToolsStream_ToolCall_Executes(t *testing.T) {
	callCount := 0
	p := &fakeStreamProvider{
		name: "fake",
		streamFn: func(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
			ch := make(chan llm.StreamEvent)
			go func() {
				callCount++
				if callCount == 1 {
					ch <- llm.StreamEvent{Type: "text", Delta: "Running tool"}
					ch <- llm.StreamEvent{
						Type:    "tool-call",
						ToolID:  "call_1",
						Tool:    "shell",
						Content: `{"command":"echo hi"}`,
					}
				} else {
					ch <- llm.StreamEvent{Type: "text", Delta: "Output: hi"}
				}
				ch <- llm.StreamEvent{Type: "done"}
				close(ch)
			}()
			return ch, nil
		},
	}
	sess := session.New()
	reg := tools.NewRegistry()
	reg.Register(&fakeTool{name: "shell", result: "hi"})

	reply, err := RunTurnWithToolsStream(
		context.Background(), p, sess,
		"operate", "", "", "test-model", "run cmd",
		reg, 5, true, nil, nil, nil, llm.ModelInfo{},
		nil, nil, nil, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if reply != "Output: hi" {
		t.Errorf("reply = %q, want %q", reply, "Output: hi")
	}
	msgs := sess.Messages()
	if len(msgs) != 4 {
		t.Fatalf("want 4 messages, got %d", len(msgs))
	}
	if msgs[2].Role != "tool" || msgs[2].Content != "hi" {
		t.Errorf("tool msg = %s/%s, want tool/hi", msgs[2].Role, msgs[2].Content)
	}
}

// TestRunTurnWithToolsStream_ToolCall_NoDelta checks tool round doesn't
// drop subsequent deltas.
func TestRunTurnWithToolsStream_ToolCall_NoDelta(t *testing.T) {
	callCount := 0
	p := &fakeStreamProvider{
		name: "fake",
		streamFn: func(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
			ch := make(chan llm.StreamEvent)
			go func() {
				callCount++
				if callCount == 1 {
					ch <- llm.StreamEvent{Type: "text", Delta: "Running tool"}
					ch <- llm.StreamEvent{
						Type:    "tool-call",
						ToolID:  "call_1",
						Tool:    "shell",
						Content: `{"command":"echo hi"}`,
					}
				} else {
					ch <- llm.StreamEvent{Type: "text", Delta: "Final answer"}
				}
				ch <- llm.StreamEvent{Type: "done"}
				close(ch)
			}()
			return ch, nil
		},
	}
	sess := session.New()
	reg := tools.NewRegistry()
	reg.Register(&fakeTool{name: "shell", result: "ok"})

	var gotDeltas []string
	reply, err := RunTurnWithToolsStream(
		context.Background(), p, sess,
		"operate", "", "", "test-model", "do it",
		reg, 5, true, nil, nil, nil, llm.ModelInfo{},
		nil,
		func(delta string) { gotDeltas = append(gotDeltas, delta) },
		nil, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if reply != "Final answer" {
		t.Errorf("reply = %q, want %q", reply, "Final answer")
	}
	// Deltas should include both text phases.
	if len(gotDeltas) < 2 {
		t.Errorf("want at least 2 deltas across both phases, got %d: %v",
			len(gotDeltas), gotDeltas)
	}
}

// TestRunTurnWithToolsStream_NoTools_ReturnsText checks no tool calls.
func TestRunTurnWithToolsStream_NoTools_ReturnsText(t *testing.T) {
	p := &fakeStreamProvider{
		name:     "fake",
		streamFn: streamTextHelper("Just text"),
	}
	sess := session.New()

	reply, err := RunTurnWithToolsStream(
		context.Background(), p, sess,
		"operate", "", "", "test-model", "hi",
		nil, 5, true, nil, nil, nil, llm.ModelInfo{},
		nil, nil, nil, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if reply != "Just text" {
		t.Errorf("reply = %q, want %q", reply, "Just text")
	}
}

// TestRunTurnWithToolsStream_MultipleToolRounds checks two sequential
// tool-call rounds work.
func TestRunTurnWithToolsStream_MultipleToolRounds(t *testing.T) {
	callCount := 0
	p := &fakeStreamProvider{
		name: "fake",
		streamFn: func(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
			ch := make(chan llm.StreamEvent)
			go func() {
				callCount++
				if callCount == 1 {
					ch <- llm.StreamEvent{Type: "text", Delta: "First call"}
					ch <- llm.StreamEvent{
						Type:    "tool-call",
						ToolID:  "call_1",
						Tool:    "shell",
						Content: `{"command":"cmd1"}`,
					}
				} else if callCount == 2 {
					ch <- llm.StreamEvent{Type: "text", Delta: "Second call"}
					ch <- llm.StreamEvent{
						Type:    "tool-call",
						ToolID:  "call_2",
						Tool:    "shell",
						Content: `{"command":"cmd2"}`,
					}
				} else {
					ch <- llm.StreamEvent{Type: "text", Delta: "Done"}
				}
				ch <- llm.StreamEvent{Type: "done"}
				close(ch)
			}()
			return ch, nil
		},
	}
	sess := session.New()
	reg := tools.NewRegistry()
	reg.Register(&fakeTool{name: "shell", result: "ok"})

	reply, err := RunTurnWithToolsStream(
		context.Background(), p, sess,
		"operate", "", "", "test-model", "do it",
		reg, 5, true, nil, nil, nil, llm.ModelInfo{},
		nil, nil, nil, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if reply != "Done" {
		t.Errorf("reply = %q, want %q", reply, "Done")
	}
	if callCount != 3 {
		t.Errorf("want 3 stream calls, got %d", callCount)
	}
	msgs := sess.Messages()
	// user + 2*(assistant+tool) + final assistant = 6
	if len(msgs) != 6 {
		t.Fatalf("want 6 messages (user+2*(assistant+tool)+assistant), got %d",
			len(msgs))
	}
}

// TestRunTurnWithToolsStream_Error_ReturnsError checks stream error.
func TestRunTurnWithToolsStream_Error_ReturnsError(t *testing.T) {
	p := &fakeStreamProvider{
		name: "fake",
		streamFn: func(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
			ch := make(chan llm.StreamEvent)
			go func() {
				ch <- llm.StreamEvent{Type: "error", Delta: "stream failed"}
				close(ch)
			}()
			return ch, nil
		},
	}
	sess := session.New()

	_, err := RunTurnWithToolsStream(
		context.Background(), p, sess,
		"operate", "", "", "test-model", "hi",
		nil, 5, true, nil, nil, nil, llm.ModelInfo{},
		nil, nil, nil, nil,
	)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "stream failed") {
		t.Errorf("want 'stream failed' in error, got %v", err)
	}
}

// TestRunTurnWithToolsStream_NilDelta_NoPanic checks nil onDelta works.
func TestRunTurnWithToolsStream_NilDelta_NoPanic(t *testing.T) {
	p := &fakeStreamProvider{
		name:     "fake",
		streamFn: streamTextHelper("hello", " world"),
	}
	sess := session.New()

	reply, err := RunTurnWithToolsStream(
		context.Background(), p, sess,
		"operate", "", "", "test-model", "hi",
		nil, 5, true, nil, nil, nil, llm.ModelInfo{},
		nil, nil, nil, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if reply != "hello world" {
		t.Errorf("reply = %q, want %q", reply, "hello world")
	}
}

// TestRunTurnWithToolsStream_OutputParity checks same output as non-streaming.
func TestRunTurnWithToolsStream_OutputParity(t *testing.T) {
	streamCallCount := 0
	pStream := &fakeStreamProvider{
		name: "fake",
		streamFn: func(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
			ch := make(chan llm.StreamEvent)
			go func() {
				streamCallCount++
				if streamCallCount == 1 {
					ch <- llm.StreamEvent{Type: "text", Delta: "Running tool"}
					ch <- llm.StreamEvent{
						Type:    "tool-call",
						ToolID:  "call_1",
						Tool:    "shell",
						Content: `{"command":"echo hi"}`,
					}
				} else {
					ch <- llm.StreamEvent{Type: "text", Delta: "Final answer: hi"}
				}
				ch <- llm.StreamEvent{Type: "done"}
				close(ch)
			}()
			return ch, nil
		},
	}
	sess := session.New()
	reg := tools.NewRegistry()
	reg.Register(&fakeTool{name: "shell", result: "hi"})

	streamReply, err := RunTurnWithToolsStream(
		context.Background(), pStream, sess,
		"operate", "", "", "test-model", "do it",
		reg, 5, true, nil, nil, nil, llm.ModelInfo{},
		nil, nil, nil, nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	// Non-streaming path.
	pStep := &fakeStepProvider{
		name: "fake",
		steps: []llm.ChatResponse{
			{
				Message: llm.Message{Role: "assistant", Content: "Running tool"},
				ToolCalls: []llm.ToolCall{
					{ID: "call_1", Name: "shell", Args: `{"command":"echo hi"}`},
				},
			},
			{
				Message: llm.Message{Role: "assistant", Content: "Final answer: hi"},
			},
		},
	}
	sess2 := session.New()

	normalReply, err := RunTurnWithTools(
		context.Background(), pStep, sess2,
		"operate", "", "", "test-model", "do it",
		reg, 5, true, nil, nil, nil, llm.ModelInfo{},
		nil, nil, nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	if streamReply != normalReply {
		t.Errorf("parity: stream = %q, normal = %q", streamReply, normalReply)
	}
}

// TestRunTurnWithToolsStream_PermissionDeny checks deny blocks execution.
func TestRunTurnWithToolsStream_PermissionDeny(t *testing.T) {
	callCount := 0
	p := &fakeStreamProvider{
		name: "fake",
		streamFn: func(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
			ch := make(chan llm.StreamEvent)
			go func() {
				callCount++
				if callCount == 1 {
					ch <- llm.StreamEvent{Type: "text", Delta: "Running"}
					ch <- llm.StreamEvent{
						Type:    "tool-call",
						ToolID:  "call_1",
						Tool:    "shell",
						Content: `{"command":"rm -rf /"}`,
					}
				} else {
					ch <- llm.StreamEvent{Type: "text", Delta: "Blocked"}
				}
				ch <- llm.StreamEvent{Type: "done"}
				close(ch)
			}()
			return ch, nil
		},
	}
	sess := session.New()
	reg := tools.NewRegistry()
	reg.Register(&fakeTool{
		name: "shell", result: "should-not-run", perm: "deny",
	})

	reply, err := RunTurnWithToolsStream(
		context.Background(), p, sess,
		"operate", "", "", "test-model", "delete",
		reg, 5, true, nil, nil, nil, llm.ModelInfo{},
		nil, nil, nil, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if reply != "Blocked" {
		t.Errorf("reply = %q, want %q", reply, "Blocked")
	}
	msgs := sess.Messages()
	if len(msgs) != 4 {
		t.Fatalf("want 4 messages, got %d", len(msgs))
	}
	if !strings.Contains(msgs[2].Content, "permission denied") {
		t.Errorf("want 'permission denied', got %q", msgs[2].Content)
	}
}

// TestRunTurnWithToolsStream_PermissionAsk_DefaultAllow checks "ask"
// with nil permFunc defaults to allow.
func TestRunTurnWithToolsStream_PermissionAsk_DefaultAllow(t *testing.T) {
	callCount := 0
	p := &fakeStreamProvider{
		name: "fake",
		streamFn: func(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
			ch := make(chan llm.StreamEvent)
			go func() {
				callCount++
				if callCount == 1 {
					ch <- llm.StreamEvent{Type: "text", Delta: "Running"}
					ch <- llm.StreamEvent{
						Type:    "tool-call",
						ToolID:  "call_1",
						Tool:    "shell",
						Content: `{"command":"echo hi"}`,
					}
				} else {
					ch <- llm.StreamEvent{Type: "text", Delta: "Output: hi"}
				}
				ch <- llm.StreamEvent{Type: "done"}
				close(ch)
			}()
			return ch, nil
		},
	}
	sess := session.New()
	reg := tools.NewRegistry()
	reg.Register(&fakeTool{
		name: "shell", result: "echo hi", perm: "ask",
	})

	reply, err := RunTurnWithToolsStream(
		context.Background(), p, sess,
		"operate", "", "", "test-model", "say hi",
		reg, 5, true, nil, nil, nil, llm.ModelInfo{},
		nil, nil, nil, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if reply != "Output: hi" {
		t.Errorf("reply = %q, want %q", reply, "Output: hi")
	}
	msgs := sess.Messages()
	if len(msgs) != 4 {
		t.Fatalf("want 4 messages, got %d", len(msgs))
	}
	if !strings.Contains(msgs[2].Content, "echo hi") {
		t.Errorf("want tool executed, got %q", msgs[2].Content)
	}
}

// TestRunTurnWithToolsStream_MaxIterations checks iteration limit.
func TestRunTurnWithToolsStream_MaxIterations(t *testing.T) {
	callCount := 0
	p := &fakeStreamProvider{
		name: "fake",
		streamFn: func(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
			ch := make(chan llm.StreamEvent)
			go func() {
				callCount++
				if callCount <= 2 {
					ch <- llm.StreamEvent{Type: "text", Delta: "Running tool"}
					ch <- llm.StreamEvent{
						Type:    "tool-call",
						ToolID:  "call_" + fmt.Sprint(callCount),
						Tool:    "shell",
						Content: `{}`,
					}
				} else {
					ch <- llm.StreamEvent{Type: "text",
						Delta: "Summary: completed"}
				}
				ch <- llm.StreamEvent{Type: "done"}
				close(ch)
			}()
			return ch, nil
		},
		chatFn: func(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
			return llm.ChatResponse{
				Message: llm.Message{Role: "assistant",
					Content: "Summary: completed by chat"},
			}, nil
		},
	}
	sess := session.New()
	reg := tools.NewRegistry()
	reg.Register(&fakeTool{name: "shell", result: "ok"})

	reply, err := RunTurnWithToolsStream(
		context.Background(), p, sess,
		"operate", "", "", "test-model", "do it",
		reg, 2, true, nil, nil, nil, llm.ModelInfo{},
		nil, nil, nil, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "Summary") {
		t.Errorf("want summary response, got %q", reply)
	}
}

// TestRunTurnWithToolsStream_DiagnoseBlocksMutating checks diagnose mode.
func TestRunTurnWithToolsStream_DiagnoseBlocksMutating(t *testing.T) {
	p := &fakeStreamProvider{
		name:     "fake",
		streamFn: streamToolCallHelper("shell", "call_1", `{"command":"rm -rf /tmp/x"}`),
		chatFn: func(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
			return llm.ChatResponse{
				Message: llm.Message{Role: "assistant", Content: "Blocked"},
			}, nil
		},
	}
	sess := session.New()
	reg := tools.NewRegistry()
	reg.Register(&fakeTool{name: "shell", result: "would-run"})

	reply, err := RunTurnWithToolsStream(
		context.Background(), p, sess,
		"diagnose", "", "", "test-model", "delete files",
		reg, 5, true, nil, nil, nil, llm.ModelInfo{},
		nil, nil, nil, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if reply != "Blocked" {
		t.Errorf("reply = %q, want %q", reply, "Blocked")
	}
	msgs := sess.Messages()
	toolMsg := msgs[2]
	if !strings.Contains(toolMsg.Content, "diagnose:") {
		t.Errorf("want 'diagnose:' in tool result, got %q", toolMsg.Content)
	}
}

// TestRunTurnWithToolsStream_UnknownTool checks unknown tool error.
func TestRunTurnWithToolsStream_UnknownTool(t *testing.T) {
	p := &fakeStreamProvider{
		name:     "fake",
		streamFn: streamToolCallHelper("nonexistent", "call_1", `{}`),
		chatFn: func(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
			return llm.ChatResponse{
				Message: llm.Message{Role: "assistant", Content: "Saw error"},
			}, nil
		},
	}
	sess := session.New()
	reg := tools.NewRegistry()

	reply, err := RunTurnWithToolsStream(
		context.Background(), p, sess,
		"operate", "", "", "test-model", "test",
		reg, 5, true, nil, nil, nil, llm.ModelInfo{},
		nil, nil, nil, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if reply != "Saw error" {
		t.Errorf("reply = %q, want %q", reply, "Saw error")
	}
	msgs := sess.Messages()
	if !strings.Contains(msgs[2].Content, "tool not found") {
		t.Errorf("want 'tool not found' in tool result, got %q", msgs[2].Content)
	}
}

// TestRunTurnWithToolsStream_OnEvent_Called checks onEvent is called
// for tool-start and tool-end.
func TestRunTurnWithToolsStream_OnEvent_Called(t *testing.T) {
	var events []AgentEvent
	callCount := 0
	p := &fakeStreamProvider{
		name: "fake",
		streamFn: func(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
			ch := make(chan llm.StreamEvent)
			go func() {
				callCount++
				if callCount == 1 {
					ch <- llm.StreamEvent{Type: "text", Delta: "Running"}
					ch <- llm.StreamEvent{
						Type:    "tool-call",
						ToolID:  "call_1",
						Tool:    "shell",
						Content: `{"command":"echo hi"}`,
					}
				} else {
					ch <- llm.StreamEvent{Type: "text", Delta: "Done"}
				}
				ch <- llm.StreamEvent{Type: "done"}
				close(ch)
			}()
			return ch, nil
		},
	}
	sess := session.New()
	reg := tools.NewRegistry()
	reg.Register(&fakeTool{name: "shell", result: "hi"})

	_, err := RunTurnWithToolsStream(
		context.Background(), p, sess,
		"operate", "", "", "test-model", "do it",
		reg, 5, true, nil, nil, nil, llm.ModelInfo{},
		func(ev AgentEvent) { events = append(events, ev) },
		nil, nil, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("want 2 events, got %d", len(events))
	}
	if events[0].Type != "tool-start" {
		t.Errorf("event[0].Type = %q, want 'tool-start'", events[0].Type)
	}
	if events[1].Type != "tool-end" {
		t.Errorf("event[1].Type = %q, want 'tool-end'", events[1].Type)
	}
}

// TestRunTurnWithToolsStream_ShellDisabled checks no tool defs for shell.
func TestRunTurnWithToolsStream_ShellDisabled(t *testing.T) {
	p := &fakeStreamProvider{
		name:     "fake",
		streamFn: streamTextHelper("Hello"),
	}
	sess := session.New()
	reg := tools.NewRegistry()
	reg.Register(&fakeTool{name: "shell", result: "ok"})

	_, err := RunTurnWithToolsStream(
		context.Background(), p, sess,
		"operate", "", "", "test-model", "hi",
		reg, 5, false, nil, nil, nil, llm.ModelInfo{},
		nil, nil, nil, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	// Should succeed with no tool calls.
	msgs := sess.Messages()
	if len(msgs) != 2 {
		t.Fatalf("want 2 messages, got %d", len(msgs))
	}
}

// TestRunTurnWithToolsStream_PermissionAllowAll_SkipsSubsequent checks
// allow-all skips subsequent permission checks for same tool.
func TestRunTurnWithToolsStream_PermissionAllowAll_SkipsSubsequent(t *testing.T) {
	var permFuncCalls []string
	p := &fakeStreamProvider{
		name: "fake",
		streamFn: func(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
			ch := make(chan llm.StreamEvent)
			go func() {
				ch <- llm.StreamEvent{Type: "text", Delta: "Running"}
				ch <- llm.StreamEvent{
					Type:    "tool-call",
					ToolID:  "call_1",
					Tool:    "shell",
					Content: `{"command":"cmd1"}`,
				}
				ch <- llm.StreamEvent{
					Type:    "tool-call",
					ToolID:  "call_2",
					Tool:    "shell",
					Content: `{"command":"cmd2"}`,
				}
				ch <- llm.StreamEvent{Type: "done"}
				close(ch)
			}()
			return ch, nil
		},
		chatFn: func(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
			return llm.ChatResponse{
				Message: llm.Message{Role: "assistant", Content: "Both ran"},
			}, nil
		},
	}
	sess := session.New()
	reg := tools.NewRegistry()
	reg.Register(&fakeTool{
		name: "shell", result: "executed", perm: "ask",
	})

	permFunc := func(toolName, command, reason string) ToolPermissionResponse {
		permFuncCalls = append(permFuncCalls, command)
		return PermAllowAll
	}

	reply, err := RunTurnWithToolsStream(
		context.Background(), p, sess,
		"operate", "", "", "test-model", "run two",
		reg, 5, true, nil, nil, nil, llm.ModelInfo{},
		nil, nil, permFunc, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if reply != "Both ran" {
		t.Errorf("reply = %q, want %q", reply, "Both ran")
	}
	if len(permFuncCalls) != 1 {
		t.Errorf("want 1 permFunc call, got %d: %v",
			len(permFuncCalls), permFuncCalls)
	}
}

// TestRunTurnWithToolsStream_EmptyStream checks empty stream returns empty.
func TestRunTurnWithToolsStream_EmptyStream(t *testing.T) {
	p := &fakeStreamProvider{
		name: "fake",
		streamFn: func(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
			ch := make(chan llm.StreamEvent)
			close(ch)
			return ch, nil
		},
	}
	sess := session.New()

	reply, err := RunTurnWithToolsStream(
		context.Background(), p, sess,
		"operate", "", "", "test-model", "hi",
		nil, 5, true, nil, nil, nil, llm.ModelInfo{},
		nil, nil, nil, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if reply != "" {
		t.Errorf("reply = %q, want ''", reply)
	}
}

// TestRunTurnWithToolsStream_PermissionAllowAll_SkipsSubsequent checks
// PermAllowAll adds tool to overrides so subsequent same-tool calls
// skip permission check.
func TestRunTurnWithTools_PermissionAllowAll_SkipsSubsequent(t *testing.T) {
	// LLM returns two tool calls for the same tool in one response,
	// then a final text response.
	toolResp := llm.ChatResponse{
		Message: llm.Message{Role: "assistant", Content: "Running..."},
		ToolCalls: []llm.ToolCall{
			{ID: "call_1", Name: "shell", Args: `{"command":"cmd1"}`},
			{ID: "call_2", Name: "shell", Args: `{"command":"cmd2"}`},
		},
	}
	textResp := llm.ChatResponse{
		Message: llm.Message{Role: "assistant", Content: "Both ran"},
	}
	p := &fakeStepProvider{
		name:  "fake",
		steps: []llm.ChatResponse{toolResp, textResp},
	}
	s := session.New()

	reg := tools.NewRegistry()
	reg.Register(&fakeTool{
		name: "shell", result: "executed", perm: "ask",
	})

	var permFuncCalls []string
	permFunc := func(toolName, command, reason string) ToolPermissionResponse {
		permFuncCalls = append(permFuncCalls, command)
		return PermAllowAll
	}

	reply, err := RunTurnWithTools(
		context.Background(), p, s,
		"operate", "", "", "m", "run two commands",
		reg, 5, true,
		nil, nil, nil, llm.ModelInfo{},
		nil,
		permFunc,
		nil,
	)
	if err != nil {
		t.Fatalf("RunTurnWithTools: %v", err)
	}
	if reply != "Both ran" {
		t.Errorf("want reply %q, got %q", "Both ran", reply)
	}

	// PermFunc should have been called only once (for cmd1 cmd2
	// should be allowed via override).
	if len(permFuncCalls) != 1 {
		t.Errorf("want 1 permFunc call, got %d: %v",
			len(permFuncCalls), permFuncCalls)
	}
	if len(permFuncCalls) > 0 && permFuncCalls[0] != "cmd1" {
		t.Errorf("first permFunc call: want 'cmd1', got %q",
			permFuncCalls[0])
	}

	msgs := s.Messages()
	if len(msgs) != 5 {
		t.Fatalf("want 5 messages (user + assistant + 2 tools + assistant), got %d",
			len(msgs))
	}
	// Both tools should have executed.
	if msgs[2].Content != "executed" {
		t.Errorf("tool 1: want 'executed', got %q", msgs[2].Content)
	}
	if msgs[3].Content != "executed" {
		t.Errorf("tool 2: want 'executed', got %q", msgs[3].Content)
	}
}
