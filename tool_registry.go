package harvey

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	anyllm "github.com/mozilla-ai/any-llm-go"
)

/** ToolRegistry manages all available schema-based tools. It is safe for
 * concurrent use.
 *
 * Example:
 *   r := NewToolRegistry()
 *   r.RegisterTool("echo", "Return the input", map[string]any{...}, handler)
 *   schemas := r.GetToolSchemas()
 */
type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]toolEntry
}

/** NewToolRegistry creates an empty ToolRegistry.
 *
 * Returns:
 *   *ToolRegistry — empty, ready to register tools.
 *
 * Example:
 *   r := NewToolRegistry()
 */
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{tools: make(map[string]toolEntry)}
}

/** RegisterTool adds or replaces a tool in the registry.
 *
 * Parameters:
 *   name        (string)         — unique function name (must be a valid identifier).
 *   description (string)         — human-readable description sent to the LLM.
 *   parameters  (map[string]any) — JSON Schema object describing the tool's parameters.
 *   handler     (ToolHandler)    — Go function that executes the tool.
 *
 * Example:
 *   r.RegisterTool("read_file", "Read a file", map[string]any{
 *       "type": "object",
 *       "properties": map[string]any{
 *           "path": map[string]any{"type": "string"},
 *       },
 *       "required": []string{"path"},
 *   }, myHandler)
 */
func (r *ToolRegistry) RegisterTool(name, description string, parameters map[string]any, handler ToolHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[name] = toolEntry{
		schema: anyllm.Tool{
			Type: "function",
			Function: anyllm.Function{
				Name:        name,
				Description: description,
				Parameters:  parameters,
			},
		},
		handler: handler,
	}
}

/** GetTool returns the schema and handler for a tool by name.
 *
 * Parameters:
 *   name (string) — the tool name.
 *
 * Returns:
 *   anyllm.Tool  — the JSON Schema definition.
 *   ToolHandler  — the executor function.
 *   bool         — false if the name is not registered.
 *
 * Example:
 *   schema, handler, ok := r.GetTool("read_file")
 */
func (r *ToolRegistry) GetTool(name string) (anyllm.Tool, ToolHandler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.tools[name]
	return e.schema, e.handler, ok
}

/** GetToolSchemas returns all registered anyllm.Tool schemas in alphabetical
 * order, ready to pass in CompletionParams.Tools.
 *
 * Returns:
 *   []anyllm.Tool — tool schema slice.
 *
 * Example:
 *   params.Tools = r.GetToolSchemas()
 */
func (r *ToolRegistry) GetToolSchemas() []anyllm.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for n := range r.tools {
		names = append(names, n)
	}
	sortStrings(names)
	schemas := make([]anyllm.Tool, len(names))
	for i, n := range names {
		schemas[i] = r.tools[n].schema
	}
	return schemas
}

/** Len returns the number of registered tools.
 *
 * Returns:
 *   int — tool count.
 *
 * Example:
 *   fmt.Println(r.Len())
 */
func (r *ToolRegistry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tools)
}

/** Dispatch looks up the named tool and executes it with args decoded from
 * the LLM's raw JSON argument string. Output is capped at maxBytes.
 *
 * Parameters:
 *   ctx      (context.Context) — controls the request lifetime.
 *   name     (string)          — tool name from the LLM's tool_call.
 *   argsJSON (string)          — raw JSON argument string from FunctionCall.Arguments.
 *   maxBytes (int)             — output size cap; 0 uses defaultMaxOutputBytes.
 *
 * Returns:
 *   string — tool result (possibly truncated).
 *   error  — if the tool is unknown, arguments are invalid, or the handler fails.
 *
 * Example:
 *   result, err := r.Dispatch(ctx, "read_file", `{"path":"main.go"}`, 65536)
 */
func (r *ToolRegistry) Dispatch(ctx context.Context, name, argsJSON string, maxBytes int) (string, error) {
	_, handler, ok := r.GetTool(name)
	if !ok {
		return "", fmt.Errorf("unknown tool %q", name)
	}

	var args map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("tool %q: invalid arguments JSON: %w", name, err)
	}

	result, err := handler(ctx, args)
	if err != nil {
		return "", err
	}
	return capOutput(result, maxBytes), nil
}

// sortStrings sorts a string slice in place (insertion sort for small slices).
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		key := s[i]
		j := i - 1
		for j >= 0 && s[j] > key {
			s[j+1] = s[j]
			j--
		}
		s[j+1] = key
	}
}
