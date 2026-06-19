package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// ToolSchema describes a tool for LLM function calling.
type ToolSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// Registry holds registered tools and dispatches calls.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
	order []string // stable registration order
}

// Creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]Tool),
	}
}

// Adds a tool by name. Panics on duplicate.
func (r *Registry) Register(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	name := t.Name()
	if _, dup := r.tools[name]; dup {
		panic(fmt.Sprintf("tool %q already registered", name))
	}
	r.tools[name] = t
	r.order = append(r.order, name)
}

// Returns tool schemas in registration order.
func (r *Registry) Schemas() []ToolSchema {
	r.mu.RLock()
	defer r.mu.RUnlock()
	schemas := make([]ToolSchema, 0, len(r.order))
	for _, name := range r.order {
		t := r.tools[name]
		schemas = append(schemas, ToolSchema{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  t.Parameters(),
		})
	}

	return schemas
}

// Lookup returns the tool by name. Returns nil, false if not found.
func (r *Registry) Lookup(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// Dispatches to the named tool. Returns error for unknown tool.
func (r *Registry) Run(
	ctx context.Context, name string, args json.RawMessage,
) (string, error) {
	r.mu.RLock()
	t, ok := r.tools[name]
	r.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", name)
	}

	return t.Run(ctx, args)
}
