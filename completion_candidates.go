package harvey

// completion_candidates.go — functions that supply argument value candidates
// for tab completion (ArgCompletion entries on Command). These must be fast:
// no LLM calls, no network I/O. File I/O is acceptable (memory store listing).

/** memorySelectItems builds a []SelectItem for all active memories in the
 * given store, suitable for picker fallbacks in /memory show, /memory forget,
 * /memory flag. Label is "ID — description"; Value is the bare ID.
 *
 * Parameters:
 *   store (*MemoryStore) — open memory store.
 *
 * Returns:
 *   []SelectItem — one entry per active memory across all types.
 *
 * Example:
 *   items := memorySelectItems(store)
 *   chosen, _ := SelectFrom(items, "Select memory: ", a.In, out)
 */
func memorySelectItems(store *MemoryStore) []SelectItem {
	var items []SelectItem
	for _, mt := range ValidMemoryTypes {
		metas, err := store.List(string(mt))
		if err != nil {
			continue
		}
		for _, m := range metas {
			label := m.ID
			if m.Description != "" {
				label = m.ID + " — " + m.Description
			}
			items = append(items, SelectItem{Value: m.ID, Label: label})
		}
	}
	return items
}

/** memoryTypeCandidates returns all valid MemoryType constant strings.
 * Called by buildCompleter for /memory list argument completion.
 *
 * Parameters:
 *   a (*Agent) — current agent (unused; kept for ArgCompletion signature).
 *
 * Returns:
 *   []string — all valid memory type names.
 *
 * Example:
 *   types := memoryTypeCandidates(nil) // ["tool_use", "workflow", ...]
 */
func memoryTypeCandidates(_ *Agent) []string {
	types := make([]string, len(ValidMemoryTypes))
	for i, t := range ValidMemoryTypes {
		types[i] = string(t)
	}
	return types
}

/** memoryIDCandidates returns IDs of all active memories across all types.
 * Opens and closes the MemoryStore on each call; intended for tab completion
 * only (not called in hot loops).
 *
 * Parameters:
 *   a (*Agent) — current agent; must have a non-nil Workspace.
 *
 * Returns:
 *   []string — memory ID strings; nil on error or empty store.
 *
 * Example:
 *   ids := memoryIDCandidates(a) // ["tool_use_a3f891", ...]
 */
func memoryIDCandidates(a *Agent) []string {
	if a.Memory == nil || a.Memory.Store == nil {
		return nil
	}
	store := a.Memory.Store

	var ids []string
	for _, mt := range ValidMemoryTypes {
		metas, err := store.List(string(mt))
		if err != nil {
			continue
		}
		for _, m := range metas {
			ids = append(ids, m.ID)
		}
	}
	return ids
}

/** routeNameCandidates returns the names of all registered remote routes.
 * Called by buildCompleter for /route rm and /route probe.
 *
 * Parameters:
 *   a (*Agent) — current agent.
 *
 * Returns:
 *   []string — route name strings; nil when Routes is nil or empty.
 *
 * Example:
 *   names := routeNameCandidates(a) // ["claude", "groq"]
 */
func routeNameCandidates(a *Agent) []string {
	if a.Routes == nil {
		return nil
	}
	names := make([]string, 0, len(a.Routes.Endpoints))
	for name := range a.Routes.Endpoints {
		names = append(names, name)
	}
	return names
}

/** skillNameCandidates returns the names of all skills in the skill catalog.
 * Called by buildCompleter for /skill load, /skill info.
 *
 * Parameters:
 *   a (*Agent) — current agent.
 *
 * Returns:
 *   []string — skill name strings; nil when no skills are loaded.
 *
 * Example:
 *   names := skillNameCandidates(a) // ["fountain-analysis", "harvey-memory"]
 */
func skillNameCandidates(a *Agent) []string {
	if len(a.Skills) == 0 {
		return nil
	}
	names := make([]string, 0, len(a.Skills))
	for name := range a.Skills {
		names = append(names, name)
	}
	return names
}

/** profileTemplateNameCandidates returns template names from ListTemplates.
 * Called by buildCompleter for /profile use argument completion.
 *
 * Parameters:
 *   a (*Agent) — current agent.
 *
 * Returns:
 *   []string — template name strings; nil when no templates are found.
 *
 * Example:
 *   names := profileTemplateNameCandidates(a) // ["Back End Developer", ...]
 */
func profileTemplateNameCandidates(a *Agent) []string {
	wsRoot := ""
	if a.Workspace != nil {
		wsRoot = a.Workspace.Root
	}
	templates := ListTemplates(wsRoot)
	if len(templates) == 0 {
		return nil
	}
	names := make([]string, len(templates))
	for i, t := range templates {
		names[i] = t.Name
	}
	return names
}
