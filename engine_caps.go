package harvey

import "slices"

/** EngineCaps states what an engine's server can and cannot do, for the
 * things Harvey would otherwise find out by asking it. A field that is
 * CapNo is not asked about: the request that would learn it is not made.
 *
 * Fields:
 *   Tools       (CapabilityStatus) — structured tool calls (a tools key in /api/chat).
 *   Embed       (CapabilityStatus) — POST /api/embed.
 *   Tokenize    (CapabilityStatus) — POST /api/tokenize.
 *   ProcessList (CapabilityStatus) — GET /api/ps, the running models.
 *   Window      (int)              — the context window every model of the engine has,
 *                                    in tokens, when the server cannot report it; 0 is
 *                                    unknown and the probe decides.
 *   NativeToolModels ([]string)      — models that take structured tool calls although
 *                                    Tools is CapNo (DR-0050); every other model of the
 *                                    engine stays in prose mode.
 *   NativeToolNames  ([]string)      — the only tools such a model is sent: the whole set
 *                                    does not fit the window (DR-0050).
 *
 * Example:
 *   if c, ok := capsForEngine("hailo"); ok && c.Tokenize == CapNo { estimate() }
 */
type EngineCaps struct {
	Tools            CapabilityStatus
	Embed            CapabilityStatus
	Tokenize         CapabilityStatus
	ProcessList      CapabilityStatus
	Window           int
	NativeToolModels []string
	NativeToolNames  []string
}

// engineCaps holds a row only for an engine whose limits are known. Ollama and
// the others have none: their servers are asked, as they always were. If
// hailo-ollama ever takes a tools key or grows an embed route, this row is
// the one place that changes.
var engineCaps = map[string]EngineCaps{
	// hailo-ollama (notes/hailo-ollama-api.md): 0.5.1 answered HTTP 500 to any
	// tools key; 5.4.0 accepts one but writes the call as <tool_call> text and
	// never sets tool_calls, so structured tools stay unavailable. There is no
	// /api/embed or /api/tokenize; GET /api/ps exists but was empty with a model
	// loaded. Every 5.4.0 model is compiled for 2,048 tokens (Hailo's
	// docs/MODELS.rst, and measured 2026-11-03: a prompt past it is silently
	// not read), and /api/show reports no window.
	//
	// Structured tool_calls is opt-in per model (DR-0050): qwen3:1.7b was right
	// 20/20 in the 2026-11-04 probes; the other five models invent calls or
	// arguments, write none, or answer HTTP 400.
	"hailo": {Tools: CapNo, Embed: CapNo, Tokenize: CapNo, ProcessList: CapNo, Window: 2048,
		NativeToolModels: []string{"qwen3:1.7b"},
		// Harvey's 15 tools are 7.6 KB; the card answers HTTP 500 past about
		// 4 KB with a system prompt (probe 2026-11-04). These three are 1.6 KB,
		// read-only, and qwen3 chose them right 15/20 with Harvey's real prompt
		// (the misses were a harmless read_file for a search).
		NativeToolNames: []string{"read_file", "list_files", "search_files"}},
}

/** capsForEngine returns the capability row for an engine.
 *
 * Parameters:
 *   engine (string) — engine label, e.g. "hailo"; "" is unknown.
 *
 * Returns:
 *   EngineCaps — the row; the zero value when ok is false.
 *   bool       — true when the engine has a row.
 *
 * Example:
 *   c, ok := capsForEngine("hailo")
 */
func capsForEngine(engine string) (EngineCaps, bool) {
	c, ok := engineCaps[engine]
	return c, ok
}

/** activeEngine returns the engine label of the active model's server: the
 * client's engine (which the per-model hef check can relabel to "hailo" for a
 * server Harvey was not told was Hailo), else the backend's name, else "".
 *
 * Returns:
 *   string — e.g. "hailo", "ollama"; "" when nothing is active.
 *
 * Example:
 *   if c, ok := capsForEngine(a.activeEngine()); ok && c.Tools == CapNo { ... }
 */
func (a *Agent) activeEngine() string {
	if ac, ok := a.Client.(*OllamaChatClient); ok && ac.Engine() != "" {
		return ac.Engine()
	}
	if a.Backend != nil {
		return a.Backend.Name()
	}
	return ""
}

/** nativeToolModel reports whether a model is on its engine's opt-in list for
 * structured tool calls (DR-0050), the one way a CapNo engine takes a tools key.
 *
 * Parameters:
 *   engine (string) — engine label, e.g. "hailo".
 *   model  (string) — bare model name, e.g. "qwen3:1.7b".
 *
 * Returns:
 *   bool — true when the engine's row lists the model.
 *
 * Example:
 *   nativeToolModel("hailo", "qwen3:1.7b") // true
 */
func nativeToolModel(engine, model string) bool {
	c, ok := capsForEngine(engine)
	return ok && slices.Contains(c.NativeToolModels, model)
}

// activeModelName is the model name of the active client, "" when there is none.
func (a *Agent) activeModelName() string {
	if ac, ok := a.Client.(*OllamaChatClient); ok {
		return ac.ModelName()
	}
	return ""
}

// activeEngineLacksTools reports whether the active engine's capability row
// says it cannot take structured tool calls, unless the active model is on the
// engine's opt-in list.
func (a *Agent) activeEngineLacksTools() bool {
	c, ok := capsForEngine(a.activeEngine())
	return ok && c.Tools == CapNo && !nativeToolModel(a.activeEngine(), a.activeModelName())
}

/** nativeToolSubset keeps, in their given order, the tools an engine's opt-in
 * models may be sent.
 *
 * Parameters:
 *   engine (string) — engine label, e.g. "hailo".
 *   tools  ([]Tool) — every tool Harvey would offer.
 *
 * Returns:
 *   []Tool — the offered tools whose names are on the engine's list; nil when none are.
 *
 * Example:
 *   tools = nativeToolSubset("hailo", tools) // read_file, list_files, search_files
 */
func nativeToolSubset(engine string, tools []Tool) []Tool {
	c, _ := capsForEngine(engine)
	var out []Tool
	for _, t := range tools {
		if slices.Contains(c.NativeToolNames, t.Function.Name) {
			out = append(out, t)
		}
	}
	return out
}
