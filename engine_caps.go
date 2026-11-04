package harvey

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
 *
 * Example:
 *   if c, ok := capsForEngine("hailo"); ok && c.Tokenize == CapNo { estimate() }
 */
type EngineCaps struct {
	Tools       CapabilityStatus
	Embed       CapabilityStatus
	Tokenize    CapabilityStatus
	ProcessList CapabilityStatus
	Window      int
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
	"hailo": {Tools: CapNo, Embed: CapNo, Tokenize: CapNo, ProcessList: CapNo, Window: 2048},
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

// activeEngineLacksTools reports whether the active engine's capability row
// says it cannot take structured tool calls.
func (a *Agent) activeEngineLacksTools() bool {
	c, ok := capsForEngine(a.activeEngine())
	return ok && c.Tools == CapNo
}
