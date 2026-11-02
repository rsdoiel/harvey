package harvey

import (
	"fmt"
	"io"
	"strings"
)

// design/memory-aware-context-window-design.md: how much context window a
// model may use on this machine.

const (
	/** windowCeiling is the most context window harvey asks for by default, in
	 * tokens. ollama.max_context changes it.
	 */
	windowCeiling = 32768

	/** windowFloor is the least harvey uses, in tokens, however little memory
	 * there is; below it the model may not fit and harvey says so.
	 */
	windowFloor = 2048

	// windowMemShare is the share of the memory available when a model is
	// selected that the model's weights and cache may take.
	windowMemShare = 0.6

	// windowStep is the granularity of a computed window, in tokens.
	windowStep = 1024
)

// numberOf reads a JSON number from a decoded model_info value.
func numberOf(v any) (float64, bool) {
	f, ok := v.(float64)
	return f, ok
}

/** kvBytesPerToken estimates how many bytes of key-value cache one token of
 * context costs a model, from the model_info /api/show reports: for every
 * layer, its KV heads times the key and value sizes, at two bytes each (the
 * default f16 cache). head_count_kv is a number or, as for a hybrid model, one
 * per layer; the sizes are attention.key_length and value_length, else the
 * embedding length divided by the head count.
 *
 * Parameters:
 *   info (map[string]any) — model_info as decoded from JSON; may be nil.
 *
 * Returns:
 *   int64 — bytes per token; 0 when the server did not say enough.
 *
 * Example:
 *   perToken := kvBytesPerToken(modelInfo) // 114688 for llama3.2:3b
 */
func kvBytesPerToken(info map[string]any) int64 {
	arch, _ := info["general.architecture"].(string)
	if arch == "" {
		return 0
	}
	get := func(k string) (float64, bool) { return numberOf(info[arch+"."+k]) }
	layers, ok := get("block_count")
	if !ok || layers < 1 {
		return 0
	}
	// KV heads summed over the layers.
	var heads float64
	switch v := info[arch+".attention.head_count_kv"].(type) {
	case float64:
		heads = v * layers
	case []any:
		for _, e := range v {
			n, ok := e.(float64)
			if !ok {
				return 0
			}
			heads += n
		}
	default:
		return 0
	}
	if heads <= 0 {
		return 0
	}
	// The key and value sizes of one head.
	keyLen, kok := get("attention.key_length")
	valLen, vok := get("attention.value_length")
	if !kok || !vok {
		emb, eok := get("embedding_length")
		nh, hok := get("attention.head_count")
		if !eok || !hok || nh < 1 {
			return 0
		}
		keyLen = emb / nh
		valLen = keyLen
	}
	if keyLen <= 0 || valLen <= 0 {
		return 0
	}
	return int64(heads * (keyLen + valLen) * 2)
}

/** WindowInputs is what SafeWindow decides from.
 *
 * Fields:
 *   ModelMax        (int)   — the window the model was trained for, in tokens; 0 = not reported.
 *   WeightsBytes    (int64) — the model's size on disk; 0 = not reported.
 *   KVBytesPerToken (int64) — see kvBytesPerToken; 0 = not known.
 *   MemAvailable    (int64) — memory available when the model was selected, in bytes.
 *   MemKnown        (bool)  — whether MemAvailable was read.
 *   Ceiling         (int)   — the most to use, in tokens; 0 = windowCeiling.
 *
 * Example:
 *   in := WindowInputs{ModelMax: 131072, WeightsBytes: 2e9, KVBytesPerToken: 114688, MemAvailable: 7 << 30, MemKnown: true}
 */
type WindowInputs struct {
	ModelMax        int
	WeightsBytes    int64
	KVBytesPerToken int64
	MemAvailable    int64
	MemKnown        bool
	Ceiling         int
}

/** WindowLimit is the context window harvey uses for a model on this machine.
 *
 * Fields:
 *   N        (int)    — tokens; 0 = unknown, and no num_ctx is sent.
 *   ModelMax (int)    — the model's trained maximum, for the notice.
 *   Reason   (string) — what set N: "model" (its own window), "ceiling", "memory", or "config" (ollama.context_length).
 *   Fits     (bool)   — false when even windowFloor is more than the memory allows.
 *   MemBytes (int64)  — the memory it was computed against, 0 when not known.
 *   Safe     (int)    — for Reason "config": what memory is estimated to hold, to warn when N is more.
 *
 * Example:
 *   // {N: 22528, ModelMax: 131072, Reason: "memory", Fits: true}
 */
type WindowLimit struct {
	N        int
	ModelMax int
	Reason   string
	Fits     bool
	MemBytes int64
	Safe     int
}

/** SafeWindow decides how much context window a model may use: the largest
 * multiple of 1,024 tokens such that its weights plus its cache for that many
 * tokens fit in 60% of the memory available when it was selected, capped by the
 * model's own window and by the ceiling, and never below windowFloor. Where the
 * memory, the cache size or the model's window is not known it does not guess:
 * it uses the ceiling, or reports 0 when the model's window is unknown.
 *
 * Parameters:
 *   in (WindowInputs) — see the type.
 *
 * Returns:
 *   WindowLimit — the window and what set it.
 *
 * Example:
 *   w := SafeWindow(WindowInputs{ModelMax: 131072, WeightsBytes: 2e9, KVBytesPerToken: 114688, MemAvailable: 7 << 30, MemKnown: true})
 *   // w.N == 22528, w.Reason == "memory"
 */
func SafeWindow(in WindowInputs) WindowLimit {
	if in.ModelMax <= 0 {
		return WindowLimit{Fits: true}
	}
	ceiling := in.Ceiling
	if ceiling <= 0 {
		ceiling = windowCeiling
	}
	w := WindowLimit{N: in.ModelMax, ModelMax: in.ModelMax, Reason: "model", Fits: true}
	if ceiling < w.N {
		w.N, w.Reason = ceiling, "ceiling"
	}
	if !in.MemKnown || in.KVBytesPerToken <= 0 {
		return w
	}
	w.MemBytes = in.MemAvailable
	budget := windowMemShare*float64(in.MemAvailable) - float64(in.WeightsBytes)
	byMemory := 0
	if budget > 0 {
		byMemory = int(budget/float64(in.KVBytesPerToken)) / windowStep * windowStep
	}
	if byMemory < w.N {
		w.N, w.Reason = byMemory, "memory"
		if w.N < windowFloor {
			w.N, w.Fits = windowFloor, false
		}
	}
	return w
}

/** Notice is the one line that tells the author a window is limited, or ""
 * when it is the model's own.
 *
 * Returns:
 *   string — such as "Context window 22,528 tokens: the model allows 131,072; memory limits it (6.9 GiB available)."
 *
 * Example:
 *   if s := limit.Notice(); s != "" { fmt.Fprintln(out, s) }
 */
func (w WindowLimit) Notice() string {
	if w.N == 0 || w.Reason == "model" || w.Reason == "" {
		return ""
	}
	why := map[string]string{
		"ceiling": "the ceiling limits it (ollama.max_context)",
		"memory":  "memory limits it",
		"config":  "ollama.context_length sets it",
	}[w.Reason]
	line := fmt.Sprintf("Context window %s tokens: the model allows %s; %s", commaInt(w.N), commaInt(w.ModelMax), why)
	if w.Reason == "config" && w.Safe > 0 && w.N > w.Safe {
		line += fmt.Sprintf(". This is more than memory is estimated to hold (about %s) and the server may be killed", commaInt(w.Safe))
	}
	if w.Reason == "memory" && w.MemBytes > 0 {
		line += fmt.Sprintf(" (%.1f GiB available)", float64(w.MemBytes)/float64(1<<30))
	}
	if !w.Fits {
		line += ". Even this may not fit in memory"
	}
	return line + "."
}

// commaInt writes n with thousands separators.
func commaInt(n int) string {
	s := fmt.Sprint(n)
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// windowKey is the key a model's limit is kept under: the model cache's.
func (a *Agent) windowKey(model string) string { return a.modelKey(model) }

/** fixWindowLimit works out the context window for a model from what it was
 * probed to be and the memory available now, and keeps it until the model is
 * selected again. It is called when an Ollama model is selected, so the number
 * does not move while a session runs: a changed num_ctx makes Ollama reload
 * the model.
 *
 * Parameters:
 *   model (string)          — the model name.
 *   cap   (*ModelCapability) — what the probe found; nil keeps nothing, so the limit is worked out later.
 *
 * Returns:
 *   WindowLimit — the limit now in force for the model.
 *
 * Example:
 *   a.fixWindowLimit("llama3.2:3b", cap)
 */
func (a *Agent) fixWindowLimit(model string, cap *ModelCapability) WindowLimit {
	if cap == nil {
		// Nothing is known about the model yet: do not keep that answer, so the
		// limit is worked out when the cache has the model.
		return WindowLimit{}
	}
	mem, ok := availableMemory()
	w := SafeWindow(WindowInputs{
		ModelMax: cap.ContextLength, WeightsBytes: cap.SizeBytes, KVBytesPerToken: cap.KVBytesPerToken,
		MemAvailable: mem, MemKnown: ok, Ceiling: a.Config.Ollama.MaxContext,
	})
	a.windowMu.Lock()
	if a.windowLimits == nil {
		a.windowLimits = map[string]WindowLimit{}
	}
	a.windowLimits[a.windowKey(model)] = w
	a.windowMu.Unlock()
	return w
}

// windowLimitFor is the limit in force for a model: the one fixed at selection,
// else one worked out now from the model cache (a model cached by an earlier
// session whose probe has not run, or failed), and the explicit
// ollama.context_length over either.
func (a *Agent) windowLimitFor(model string) WindowLimit {
	key := a.windowKey(model)
	a.windowMu.Lock()
	w, ok := a.windowLimits[key]
	a.windowMu.Unlock()
	if !ok {
		var cap *ModelCapability
		if a.ModelCache != nil {
			cap, _ = a.ModelCache.Get(key)
		}
		w = a.fixWindowLimit(model, cap)
	}
	if n := a.Config.Ollama.ContextLength; n > 0 {
		return WindowLimit{N: n, ModelMax: w.ModelMax, Reason: "config", Fits: true, MemBytes: w.MemBytes, Safe: w.N}
	}
	return w
}

/** WindowLimit is the context window limit in force for the active model.
 *
 * Returns:
 *   WindowLimit — N is 0 when the model's window is not known.
 *
 * Example:
 *   if s := a.WindowLimit().Notice(); s != "" { fmt.Fprintln(out, s) }
 */
func (a *Agent) WindowLimit() WindowLimit {
	return a.windowLimitFor(a.Config.Ollama.Model)
}

// logWindow is the window the session log records for the active model, with
// what set it: "memory", "ceiling", "model" (its own), "configured", or
// "unknown" when the model's window is not known.
func (a *Agent) logWindow() Window {
	w := a.WindowLimit()
	src := map[string]string{"memory": "memory", "ceiling": "ceiling", "model": "model", "config": "configured"}[w.Reason]
	if w.N == 0 || src == "" {
		src = "unknown"
	}
	return Window{N: w.N, Source: src}
}

/** noteWindow tells the author when the active model's context window is
 * limited, in one line, and says nothing when it is the model's own. It is
 * called where a model is announced: at startup and at a switch.
 *
 * Parameters:
 *   out (io.Writer) — where the line is written.
 *
 * Example:
 *   fmt.Fprintf(out, "  Using model: %s\n", name)
 *   a.noteWindow(out)
 */
func (a *Agent) noteWindow(out io.Writer) {
	if _, ok := a.Client.(*OllamaChatClient); !ok {
		return
	}
	if n := a.WindowLimit().Notice(); n != "" {
		fmt.Fprintf(out, "  %s\n", dim(n))
	}
}
