package harvey

import (
	"io"
	"strings"
)

// dispatch_target.go — a single, correct model-resolution primitive shared
// by /plan next, /read-chunks, injectOrChunk's auto-chunk path, and (in a
// later phase) @mention's registry dispatch. See
// subagent-dispatch-design.md (Direction D) for the audit this replaces:
// three independent, and in two cases buggy, ways of resolving "which
// LLMClient should run this bounded turn."

/** DispatchTarget is a resolved model to run one bounded turn against.
 *
 * Fields:
 *   Client  (LLMClient) — the client to dispatch this turn's messages to.
 *   Restore (func())    — call after the turn completes. No-op for a route
 *     endpoint or an already-active local model (neither mutated Agent
 *     state); for a local model switch, relaunches the pre-switch model —
 *     a real, non-trivial cost (see subagent-dispatch-design.md), not a
 *     cheap reference swap.
 *
 * Example:
 *   target, ok, err := resolveDispatchTarget(a, "granite", out)
 *   if ok {
 *       defer target.Restore()
 *       reply, err := target.Client.Chat(ctx, messages, w)
 *   }
 */
type DispatchTarget struct {
	Client  LLMClient
	Restore func()
}

// currentActiveName returns the name that identifies the agent's currently
// active local model for lookup purposes: the llamafile registry name when
// set, else the configured Ollama model. Empty when neither is set.
func currentActiveName(a *Agent) string {
	if a.Config.Llamafile.Active != "" {
		return a.Config.Llamafile.Active
	}
	return a.Config.Ollama.Model
}

/** resolveDispatchTarget resolves name (an @mention-style identifier, or a
 * plan step's [model: NAME] annotation) to a DispatchTarget.
 *
 * Resolution order:
 *   1. A registered route endpoint (a.Routes) — returns an independent
 *      LLMClient via clientForEndpoint; never mutates a.Client/a.Backend;
 *      Restore is a no-op.
 *   2. name already matches the active local model (case-insensitive) —
 *      no switch needed; Restore is a no-op.
 *   3. A local llamafile registry entry or model alias — performs a real,
 *      persistent switch via attemptModelSwitch (or
 *      a.attemptModelSwitchOverride in tests), capturing the pre-switch
 *      state first so Restore can correctly relaunch it afterward.
 *
 * Parameters:
 *   a    (*Agent)    — the running agent.
 *   name (string)    — identifier to resolve.
 *   out  (io.Writer) — destination for switch progress/status output.
 *
 * Returns:
 *   DispatchTarget — resolved client and restore function; zero value when !ok.
 *   bool           — true when name was resolved to a target.
 *   error          — non-nil on a resolution or switch failure for a name
 *     that was otherwise found (as opposed to simply not matching anything).
 *
 * Example:
 *   target, ok, err := resolveDispatchTarget(a, "granite", out)
 *   if err != nil { return err }
 *   if !ok { fmt.Fprintf(out, "model %q not found\n", "granite") }
 */
func resolveDispatchTarget(a *Agent, name string, out io.Writer) (DispatchTarget, bool, error) {
	noop := func() {}

	if a.Routes != nil && a.Routes.Enabled {
		if ep := a.Routes.Lookup(name); ep != nil {
			client, err := clientForEndpoint(ep, a.Config)
			if err != nil {
				return DispatchTarget{}, true, err
			}
			return DispatchTarget{Client: client, Restore: noop}, true, nil
		}
	}

	if active := currentActiveName(a); active != "" && strings.EqualFold(active, name) {
		return DispatchTarget{Client: a.Client, Restore: noop}, true, nil
	}

	switchFn := func(n string, w io.Writer) (bool, error) { return attemptModelSwitch(a, n, w) }
	if a.attemptModelSwitchOverride != nil {
		switchFn = a.attemptModelSwitchOverride
	}

	// Captured BEFORE switching — switchFn (attemptModelSwitch's llamafile
	// branch, via switchLlamafileModel) overwrites a.Config.Llamafile.Active
	// as part of performing the switch, so this must be read now, not later.
	prevLlamafileActive := a.Config.Llamafile.Active
	prevOllamaModel := a.Config.Ollama.Model

	switched, err := switchFn(name, out)
	if err != nil {
		return DispatchTarget{}, true, err
	}
	if !switched {
		return DispatchTarget{}, false, nil
	}

	restore := func() {
		if prevLlamafileActive != "" {
			_, _ = switchFn(prevLlamafileActive, out)
			return
		}
		a.Config.Ollama.Model = prevOllamaModel
		a.Client = newOllamaLLMClient(a.Config.Ollama.URL, prevOllamaModel, a.Config.Ollama.Timeout)
	}

	return DispatchTarget{Client: a.Client, Restore: restore}, true, nil
}
