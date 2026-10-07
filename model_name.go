package harvey

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// modelEngines are the engine prefixes a model name may carry. Only these are
// read as an engine: a slash elsewhere ("user/model:tag") is part of the name.
var modelEngines = []string{"hailo", "ollama", "llamafile", "llamacpp"}

/** parseQualifiedModel splits "engine/model" into its engine and model. Only a
 * known engine prefix (hailo, ollama, llamafile, llamacpp) counts, matched
 * without regard to case; any other name, including one with a slash in it,
 * is returned whole with an empty engine.
 *
 * Parameters:
 *   name (string) — what the user typed, e.g. "hailo/llama3.2:3b" or "phi4".
 *
 * Returns:
 *   string — the engine, lower case; "" when the name is not qualified.
 *   string — the model name, without the engine prefix when there was one.
 *
 * Example:
 *   engine, model := parseQualifiedModel("hailo/llama3.2:3b") // "hailo", "llama3.2:3b"
 */
func parseQualifiedModel(name string) (string, string) {
	i := strings.Index(name, "/")
	if i <= 0 || i == len(name)-1 {
		return "", name
	}
	for _, e := range modelEngines {
		if strings.EqualFold(name[:i], e) {
			return e, name[i+1:]
		}
	}
	return "", name
}

// qualifiedName is m in the form accepted wherever a model is named.
func qualifiedName(m ModelSummary) string { return m.Engine + "/" + m.Name }

// sameNameOnSeveralEngines reports whether every candidate has the same name
// (ignoring case) and there is more than one: the same model name on two engines.
func sameNameOnSeveralEngines(cands []ModelSummary) bool {
	if len(cands) < 2 {
		return false
	}
	for _, c := range cands[1:] {
		if !strings.EqualFold(c.Name, cands[0].Name) {
			return false
		}
	}
	return true
}

// ambiguousModelError is the usage error for a name that matches several
// models; it names each one in the form that selects it.
func ambiguousModelError(query string, cands []ModelSummary) error {
	names := make([]string, len(cands))
	for i, c := range cands {
		names[i] = qualifiedName(c)
	}
	return Usagef("%q matches several models; name one of: %s", query, strings.Join(names, ", "))
}

// attended reports whether a person is at a terminal and can be asked.
func (a *Agent) attended() bool {
	in := a.stdin
	if in == nil {
		in = os.Stdin
	}
	return a.interactiveSession(in)
}

/** chooseAmong resolves a model name that exists on several engines. At a
 * terminal it lists the candidates in their qualified form and asks which; with
 * nobody to ask it is a usage error that names them. It never picks silently.
 *
 * Parameters:
 *   query (string)         — what the user typed.
 *   cands ([]ModelSummary) — the models it matched.
 *   out   (io.Writer)      — destination for the question.
 *
 * Returns:
 *   ModelSummary — the chosen model.
 *   error        — usage when unattended or the answer is not a listed number.
 *
 * Example:
 *   m, err := a.chooseAmong("llama3.2:3b", cands, out)
 */
func (a *Agent) chooseAmong(query string, cands []ModelSummary, out io.Writer) (ModelSummary, error) {
	if !a.attended() {
		return ModelSummary{}, ambiguousModelError(query, cands)
	}
	fmt.Fprintf(out, "  %q is on more than one engine:\n", query)
	for i, c := range cands {
		fmt.Fprintf(out, "  [%d] %s\n", i+1, qualifiedName(c))
	}
	fmt.Fprintf(out, "    Select model [1-%d]: ", len(cands))
	var n int
	if _, err := fmt.Sscanf(strings.TrimSpace(readLineFrom(a.In)), "%d", &n); err != nil || n < 1 || n > len(cands) {
		return ModelSummary{}, Usagef("no model chosen; name one of: %s", strings.Join(qualifiedNames(cands), ", "))
	}
	return cands[n-1], nil
}

func qualifiedNames(cands []ModelSummary) []string {
	out := make([]string, len(cands))
	for i, c := range cands {
		out[i] = qualifiedName(c)
	}
	return out
}

/** cacheKeyFor returns the model cache key for a model the user named: the
 * engine in a qualified name, otherwise the active engine.
 *
 * Parameters:
 *   name (string) — "engine/model" or a bare model name.
 *
 * Returns:
 *   string — the key for ModelCache.Get and Set.
 *
 * Example:
 *   cap, _ := a.ModelCache.Get(a.cacheKeyFor("hailo/llama3.2:3b")) // key "hailo/llama3.2:3b"
 */
func (a *Agent) cacheKeyFor(name string) string {
	if engine, model := parseQualifiedModel(name); engine != "" {
		return modelKey(engine, model)
	}
	return a.modelKey(name)
}

/** useStartupModel applies a model named on the command line (-m) or in
 * ollama.model. A qualified name goes to its engine. A bare name goes to the
 * engine that has it; on both Ollama and Hailo it asks at a terminal and is a
 * usage error otherwise. A name neither server lists is passed to Ollama as it
 * always was.
 *
 * Parameters:
 *   name (string)    — the value of -m or ollama.model.
 *   out  (io.Writer) — destination for any question.
 *
 * Returns:
 *   error — usage for an ambiguous or unsupported name; unavailable when the
 *           named engine's server is not running.
 *
 * Example:
 *   err := a.useStartupModel("hailo/llama3.2:3b", out)
 */
func (a *Agent) useStartupModel(name string, out io.Writer) error {
	engine, model := parseQualifiedModel(name)
	switch engine {
	case "hailo":
		return a.setHailoModel(model)
	case "ollama":
		a.setOllamaModel(model)
		return nil
	case "llamafile", "llamacpp":
		return Usagef("-m %s: %s models are chosen with /model use, not at start", name, engine)
	}
	var cands []ModelSummary
	for _, m := range a.listFamilyModels() {
		if strings.EqualFold(m.Name, model) {
			cands = append(cands, m)
		}
	}
	if len(cands) > 1 {
		m, err := a.chooseAmong(name, cands, out)
		if err != nil {
			return err
		}
		cands = []ModelSummary{m}
	}
	if len(cands) == 1 && cands[0].Engine == "hailo" {
		return a.setHailoModel(model)
	}
	a.setOllamaModel(model)
	return nil
}
