package harvey

/** ProfileToolSet returns the tool definitions assay offers a model when it
 * probes tool calling: Harvey's own read-only subset (the tools a hailo opt-in
 * model is sent, DR-0050), built from the real registrations so the schemas
 * cannot drift from what a session sends.
 *
 * Returns:
 *   []Tool — read_file, list_files and search_files, sorted by name, as the registry returns them.
 *
 * Example:
 *   tools := harvey.ProfileToolSet()
 *   stats, calls, err := client.ChatWithTools(ctx, msgs, tools, w)
 */
func ProfileToolSet() []Tool {
	r := NewToolRegistry()
	RegisterBuiltinTools(r, &Agent{Workspace: &Workspace{Root: "."}, Config: &Config{}})
	return nativeToolSubset("hailo", r.GetToolSchemas())
}

/** ProseToolCallsIn reads the tool calls a reply wrote as JSON code blocks
 * (the prose form Harvey parses: {"name": ..., "arguments"|"parameters": ...}).
 *
 * Parameters:
 *   text (string) — the model's reply
 *
 * Returns:
 *   []ToolCall — the calls found; nil when none
 *
 * Example:
 *   calls := harvey.ProseToolCallsIn("```json\n{\"name\":\"read_file\",\"arguments\":{\"path\":\"a\"}}\n```")
 */
func ProseToolCallsIn(text string) []ToolCall {
	return ParseProseToolCalls(extractCodeBlocks(text))
}

/** EstimateTokens estimates how many tokens text is, the way Harvey budgets a
 * prompt. It is an estimate; an engine's own tokenizer will differ.
 *
 * Parameters:
 *   s (string) — the text
 *
 * Returns:
 *   int — estimated tokens: length/4, at least 1
 *
 * Example:
 *   n := harvey.EstimateTokens(prompt)
 */
func EstimateTokens(s string) int { return estimateTokens(s) }
