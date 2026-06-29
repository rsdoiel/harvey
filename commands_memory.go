// Package harvey — commands_memory.go implements the /memory slash command
// family for managing Harvey's experience memory store.
package harvey

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/term"
)

// ── /memory ──────────────────────────────────────────────────────────────────

/** cmdMemory dispatches /memory subcommands: mine, list, show, flag, forget,
 * status, recall, profile.
 *
 * Parameters:
 *   a    (*Agent)    — Harvey agent.
 *   args ([]string)  — subcommand and arguments.
 *   out  (io.Writer) — output writer.
 *
 * Returns:
 *   error — on store open or subcommand failure.
 *
 * Example:
 *   /memory mine
 *   /memory list --type tool_use --kind pitfall
 *   /memory show git_fix_a3f891
 *   /memory flag git_fix_a3f891
 *   /memory forget git_fix_a3f891
 *   /memory status
 */
func cmdMemory(a *Agent, args []string, out io.Writer) error {
	if len(args) == 0 {
		fmt.Fprintln(out, "Usage: /memory <mine|list|show|flag|forget|status|recall|profile> [args...]")
		return nil
	}
	if a.Memory == nil || a.Memory.Store == nil {
		return fmt.Errorf("/memory: memory store not available")
	}
	store := a.Memory.Store

	switch args[0] {
	case "mine":
		return cmdMemoryMine(a, args[1:], out, store)
	case "list":
		return cmdMemoryList(a, args[1:], out, store)
	case "show":
		return cmdMemoryShow(a, args[1:], out, store)
	case "flag":
		return cmdMemoryFlag(a, args[1:], out, store)
	case "forget":
		return cmdMemoryForget(a, args[1:], out, store)
	case "status":
		return cmdMemoryStatus(a, args[1:], out, store)
	case "recall":
		return cmdMemoryRecall(a, args[1:], out, store)
	case "profile":
		return cmdMemoryProfile(a, args[1:], out, store)
	default:
		fmt.Fprintf(out, "Unknown /memory subcommand: %q\n", args[0])
		fmt.Fprintln(out, "Usage: /memory <mine|list|show|flag|forget|status|recall|profile> [args...]")
		return nil
	}
}

// cmdMemoryMine mines a session file for memories with interactive review.
func cmdMemoryMine(a *Agent, args []string, out io.Writer, store *MemoryStore) error {
	force := false
	var sessionPath string
	for _, arg := range args {
		if arg == "--force" {
			force = true
		} else {
			sessionPath = arg
		}
	}

	manifest, err := LoadManifest(store.Dir())
	if err != nil {
		return fmt.Errorf("memory mine: load manifest: %w", err)
	}

	if sessionPath == "" {
		sessDir := a.SessionsDir
		if sessDir == "" {
			sessDir = filepath.Join(a.Workspace.Root, harveySubdir, "sessions")
		}
		var candidates []string
		if force {
			entries, _ := os.ReadDir(sessDir)
			for _, e := range entries {
				if !e.IsDir() && (filepath.Ext(e.Name()) == ".spmd" || filepath.Ext(e.Name()) == ".fountain") {
					candidates = append(candidates, filepath.Join(sessDir, e.Name()))
				}
			}
			sort.Strings(candidates)
		} else {
			candidates, err = manifest.UnminedSessions(sessDir)
			if err != nil {
				return fmt.Errorf("memory mine: list sessions: %w", err)
			}
		}
		if len(candidates) == 0 {
			fmt.Fprintln(out, "No unmined sessions found.")
			return nil
		}
		sessionPath = candidates[len(candidates)-1]
	}

	if force {
		filtered := manifest.Sessions[:0]
		for _, e := range manifest.Sessions {
			if e.Path != sessionPath {
				filtered = append(filtered, e)
			}
		}
		manifest.Sessions = filtered
	}

	var embedder Embedder
	if entry := a.Config.Memory.ActiveRagStore(); entry != nil {
		embedder = NewEmbedderForEntry(entry, a.Config.Ollama.URL)
	}

	miner := NewMiner(store, manifest, a.Workspace)
	return miner.Mine(context.Background(), sessionPath, a, embedder, out, a.In)
}

// cmdMemoryList lists non-archived memories, optionally filtered by type or kind.
func cmdMemoryList(a *Agent, args []string, out io.Writer, store *MemoryStore) error {
	typeFilter := ""
	kindFilter := ""
	for i, arg := range args {
		if arg == "--type" && i+1 < len(args) {
			typeFilter = args[i+1]
		}
		if arg == "--kind" && i+1 < len(args) {
			kindFilter = args[i+1]
		}
	}
	metas, err := store.List(typeFilter)
	if err != nil {
		return fmt.Errorf("memory list: %w", err)
	}
	if len(metas) == 0 {
		fmt.Fprintln(out, "No memories found.")
		return nil
	}
	for _, m := range metas {
		if kindFilter != "" && m.Kind != kindFilter {
			continue
		}
		kind := m.Kind
		if kind == "" {
			kind = "-"
		}
		fmt.Fprintf(out, "%-30s  %-16s  %-14s  %.1f  %s\n",
			m.ID, m.Type, kind, m.Confidence, m.Description)
	}
	return nil
}

// cmdMemoryShow displays the full content of a memory by ID.
func cmdMemoryShow(a *Agent, args []string, out io.Writer, store *MemoryStore) error {
	if len(args) == 0 {
		items := memorySelectItems(store)
		if len(items) == 0 {
			fmt.Fprintln(out, "No memories found.")
			return nil
		}
		chosen, err := SelectFrom(items, fmt.Sprintf("Show which memory [1-%d] or Enter to cancel: ", len(items)), a.In, out)
		if err != nil || chosen == "" {
			return err
		}
		args = []string{chosen}
	}
	doc, err := store.ByID(args[0])
	if err != nil {
		return fmt.Errorf("memory show: %w", err)
	}
	if doc == nil {
		fmt.Fprintf(out, "Memory %q not found.\n", args[0])
		return nil
	}
	data, err := doc.Bytes()
	if err != nil {
		return fmt.Errorf("memory show: serialise: %w", err)
	}
	fmt.Fprint(out, string(data))
	return nil
}

// cmdMemoryForget archives a memory by ID immediately.
func cmdMemoryForget(a *Agent, args []string, out io.Writer, store *MemoryStore) error {
	if len(args) == 0 {
		items := memorySelectItems(store)
		if len(items) == 0 {
			fmt.Fprintln(out, "No memories found.")
			return nil
		}
		chosen, err := SelectFrom(items, fmt.Sprintf("Forget which memory [1-%d] or Enter to cancel: ", len(items)), a.In, out)
		if err != nil || chosen == "" {
			return err
		}
		args = []string{chosen}
	}
	if err := store.Archive(args[0]); err != nil {
		return fmt.Errorf("memory forget: %w", err)
	}
	fmt.Fprintf(out, "Memory %q archived.\n", args[0])
	return nil
}

// cmdMemoryFlag reduces a memory's confidence by 0.1. When confidence falls
// to or below 0.2 the memory is auto-archived. Use /memory forget for
// immediate archival without the confidence step.
func cmdMemoryFlag(a *Agent, args []string, out io.Writer, store *MemoryStore) error {
	if len(args) == 0 {
		items := memorySelectItems(store)
		if len(items) == 0 {
			fmt.Fprintln(out, "No memories found.")
			return nil
		}
		chosen, err := SelectFrom(items, fmt.Sprintf("Flag which memory [1-%d] or Enter to cancel: ", len(items)), a.In, out)
		if err != nil || chosen == "" {
			return err
		}
		args = []string{chosen}
	}
	id := args[0]
	newConf, err := store.SetConfidence(id, -0.1)
	if errors.Is(err, ErrAutoArchived) {
		fmt.Fprintf(out, "%s: confidence → %.1f — auto-archived (below threshold)\n", id, newConf)
		return nil
	}
	if err != nil {
		return fmt.Errorf("memory flag: %w", err)
	}
	fmt.Fprintf(out, "%s: confidence → %.1f\n", id, newConf)
	return nil
}

// cmdMemoryStatus shows manifest summary and memory counts.
func cmdMemoryStatus(a *Agent, args []string, out io.Writer, store *MemoryStore) error {
	n, err := store.Count()
	if err != nil {
		return fmt.Errorf("memory status: count: %w", err)
	}

	manifest, err := LoadManifest(store.Dir())
	if err != nil {
		return fmt.Errorf("memory status: load manifest: %w", err)
	}

	sessDir := a.SessionsDir
	if sessDir == "" {
		sessDir = filepath.Join(a.Workspace.Root, harveySubdir, "sessions")
	}
	unmined, _ := manifest.UnminedSessions(sessDir)

	totalCreated := 0
	for _, e := range manifest.Sessions {
		totalCreated += len(e.MemoriesCreated)
	}

	fmt.Fprintf(out, "Memory store:    %s\n", store.Dir())
	fmt.Fprintf(out, "Active memories: %d\n", n)
	fmt.Fprintf(out, "Sessions mined:  %d  (total memories created: %d)\n", len(manifest.Sessions), totalCreated)
	fmt.Fprintf(out, "Sessions pending: %d\n", len(unmined))

	// Budget stats.
	budgetPct := a.Config.Memory.BudgetPct
	contextLen := a.effectiveContextLimit()
	modelName := "unknown"
	if a.Client != nil {
		modelName = a.Client.Name()
	}
	if contextLen > 0 {
		budget := int(float64(contextLen) * budgetPct)
		fmt.Fprintf(out, "Memory budget:   %.0f%% of context (%d tokens on %s)\n",
			budgetPct*100, budget, modelName)
	} else {
		fmt.Fprintf(out, "Memory budget:   %.0f%% of context (context window unknown)\n",
			budgetPct*100)
	}

	statsCount, _ := store.StatsCount()
	if statsCount >= 10 {
		avgSat, compRate, avgTps, statsErr := store.BudgetStats(10)
		if statsErr == nil {
			satPct := int(avgSat * 100)
			switch {
			case avgSat > 0.90 && (avgTps == 0 || avgTps >= 2.0):
				newPct := budgetPct * 1.4
				fmt.Fprintf(out, "Budget advice:   avg utilisation %d%% over last 10 sessions —\n", satPct)
				fmt.Fprintf(out, "                 consider increasing memory.budget_pct to %.2f\n", newPct)
			case avgTps > 0 && avgTps < 2.0 && avgSat > 0.70:
				newPct := budgetPct * 0.75
				fmt.Fprintf(out, "Budget advice:   avg utilisation %d%%, throughput %.1f tok/s — context pressure high;\n", satPct, avgTps)
				fmt.Fprintf(out, "                 consider reducing memory.budget_pct to %.2f\n", newPct)
			default:
				fmt.Fprintf(out, "Budget advice:   avg utilisation %d%% — current setting looks good\n", satPct)
			}
			if compRate > 0.50 {
				fmt.Fprintf(out, "Compression:     rolling summary fired in %.0f%% of recent sessions\n", compRate*100)
			}
		}
	}
	return nil
}

// cmdMemoryRecall queries all memory silos and prints grouped results.
func cmdMemoryRecall(a *Agent, args []string, out io.Writer, store *MemoryStore) error {
	if len(args) == 0 {
		fmt.Fprintln(out, "Usage: /memory recall <query>")
		return nil
	}
	query := strings.Join(args, " ")

	var embedder Embedder
	if entry := a.Config.Memory.ActiveRagStore(); entry != nil {
		embedder = NewEmbedderForEntry(entry, a.Config.Ollama.URL)
	}

	results, err := a.Memory.Unified.Recall(query, embedder, 0)
	if err != nil {
		return fmt.Errorf("memory recall: %w", err)
	}
	if len(results) == 0 {
		fmt.Fprintln(out, "No memories found.")
		return nil
	}

	curSource := ""
	for _, r := range results {
		if r.Source != curSource {
			if curSource != "" {
				fmt.Fprintln(out)
			}
			fmt.Fprintf(out, "[%s]\n", sourceHeader(r.Source))
			curSource = r.Source
		}
		fmt.Fprintf(out, "  [%.2f] %s\n", r.Score, r.Content)
	}
	return nil
}

// cmdMemoryProfile dispatches /memory profile subcommands.
func cmdMemoryProfile(a *Agent, args []string, out io.Writer, store *MemoryStore) error {
	sub := "list"
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "list":
		var rest []string
		if len(args) > 1 {
			rest = args[1:]
		}
		return cmdMemoryProfileList(a, rest, out, store)
	case "show":
		return cmdMemoryProfileShowContent(a, out, store)
	case "edit":
		return cmdMemoryProfileUpdate(a, out, store)
	case "update":
		fmt.Fprintln(out, dim("  ⚠  /memory profile update is deprecated; use /memory profile edit"))
		return cmdMemoryProfileUpdate(a, out, store)
	case "use":
		return cmdMemoryProfileUse(a, args[1:], out, store)
	case "rename":
		return cmdMemoryProfileRename(a, args[1:], out, store)
	default:
		fmt.Fprintf(out, "Usage: /memory profile <list|show|edit|use|rename> [args...]\n")
		return nil
	}
}

// cmdMemoryProfileList lists active and archived workspace profiles (old "show" behavior).
func cmdMemoryProfileList(a *Agent, args []string, out io.Writer, store *MemoryStore) error {
	return cmdMemoryList(a, append([]string{"--type", string(MemoryTypeWorkspaceProfile)}, args...), out, store)
}

// cmdMemoryProfileShowContent prints the full content of the active workspace profile.
func cmdMemoryProfileShowContent(a *Agent, out io.Writer, store *MemoryStore) error {
	metas, err := store.List(string(MemoryTypeWorkspaceProfile))
	if err != nil {
		return err
	}
	if len(metas) == 0 {
		fmt.Fprintln(out, "  No workspace profiles found. Run /profile use to set one.")
		return nil
	}
	active := metas[0]
	doc, err := store.ByID(active.ID)
	if err != nil {
		return fmt.Errorf("profile show: %w", err)
	}
	if doc == nil {
		fmt.Fprintln(out, "  Profile document not found on disk.")
		return nil
	}
	fmt.Fprintf(out, "\nActive workspace profile: %s (%s)\n\n", active.Description, active.ID)
	fmt.Fprintln(out, strings.Repeat("─", 60))
	fmt.Fprintln(out, strings.TrimSpace(doc.FountainBody))
	fmt.Fprintln(out, strings.Repeat("─", 60))

	// RAG context summary — shown when a store is active and has chunks.
	if a.Rag != nil {
		if n, err := a.Rag.Count(); err == nil && n > 0 {
			storeName := ""
			if entry := a.Config.Memory.ActiveRagStore(); entry != nil {
				storeName = entry.Name
			}
			if a.RagOn {
				fmt.Fprintf(out, "\nRAG context: %s (%d chunk(s), on)\n", storeName, n)
			} else {
				fmt.Fprintf(out, "\nRAG context: %s (%d chunk(s), off — /rag on to enable)\n", storeName, n)
			}
		}
	}
	return nil
}

// cmdMemoryProfileRename updates the description/title of the active workspace profile.
func cmdMemoryProfileRename(a *Agent, args []string, out io.Writer, store *MemoryStore) error {
	if len(args) == 0 {
		fmt.Fprintln(out, "Usage: /memory profile rename NAME")
		return nil
	}
	newName := strings.Join(args, " ")
	metas, err := store.List(string(MemoryTypeWorkspaceProfile))
	if err != nil || len(metas) == 0 {
		fmt.Fprintln(out, "  No active workspace profile to rename.")
		return nil
	}
	active := metas[0]
	doc, err := store.ByID(active.ID)
	if err != nil {
		return fmt.Errorf("profile rename: %w", err)
	}
	if doc == nil {
		fmt.Fprintln(out, "  Profile document not found on disk.")
		return nil
	}
	doc.Meta.Description = newName
	doc.FountainBody = rewriteProfileTitle(doc.FountainBody, newName)
	var embedder Embedder
	if entry := a.Config.Memory.ActiveRagStore(); entry != nil {
		embedder = NewEmbedderForEntry(entry, a.Config.Ollama.URL)
	}
	if err := store.Save(doc, embedder); err != nil {
		return fmt.Errorf("profile rename: %w", err)
	}
	fmt.Fprintf(out, green("✓")+" Workspace renamed to %q\n", newName)
	return nil
}

// rewriteProfileTitle replaces the TITLE: field or INT. scene heading in a
// Fountain profile body with newName. TITLE: takes priority; if absent, the
// INT. WORKSPACE PROFILE line is updated (using an uppercased name).
func rewriteProfileTitle(body, newName string) string {
	lines := strings.Split(body, "\n")
	upper := strings.ToUpper(newName)
	// First pass: prefer the explicit TITLE: field.
	for i, line := range lines {
		if strings.HasPrefix(line, "TITLE:") {
			lines[i] = "TITLE: " + newName
			return strings.Join(lines, "\n")
		}
	}
	// Second pass: fall back to the INT. scene heading.
	for i, line := range lines {
		if strings.HasPrefix(line, "INT. WORKSPACE PROFILE") {
			lines[i] = "INT. WORKSPACE PROFILE - " + upper
			return strings.Join(lines, "\n")
		}
	}
	return body
}

// cmdMemoryProfileUse switches to a new workspace profile:
//  1. Writes a structural handoff document to agents/hand-off/.
//  2. Archives the current active workspace_profile memories.
//  3. Selects a template (by name or interactive picker) and saves it as the
//     new workspace_profile.
//  4. Calls ClearHistory() so the new profile is injected on the next turn.
func cmdMemoryProfileUse(a *Agent, args []string, out io.Writer, store *MemoryStore) error {
	// Step 1 — write handoff (non-fatal).
	if a.Workspace != nil {
		if handoffDir, err := ResolveHandoffDir(a.Workspace); err == nil {
			if path, err := a.WriteHandoff(store, handoffDir); err != nil {
				fmt.Fprintf(out, yellow("  ✗")+" Handoff: %v\n", err)
			} else {
				fmt.Fprintf(out, dim("  Handoff saved: %s\n"), filepath.Base(path))
			}
		}
	}

	// Step 2 — archive existing active profiles (non-fatal).
	if metas, err := store.List(string(MemoryTypeWorkspaceProfile)); err == nil {
		for _, m := range metas {
			if archErr := store.Archive(m.ID); archErr != nil {
				fmt.Fprintf(out, yellow("  ✗")+" Archive %s: %v\n", m.ID, archErr)
			}
		}
	}

	// Step 3 — select template and save new profile.
	wsRoot := ""
	if a.Workspace != nil {
		wsRoot = a.Workspace.Root
	}
	chosen, templateName := profileSelectTemplate(a, args, wsRoot, out)

	var embedder Embedder
	if entry := a.Config.Memory.ActiveRagStore(); entry != nil {
		embedder = NewEmbedderForEntry(entry, a.Config.Ollama.URL)
	}

	wsName := filepath.Base(wsRoot)
	if wsName == "" || wsName == "." {
		wsName = "workspace"
	}
	id := GenerateMemoryID(MemoryTypeWorkspaceProfile)
	description := templateName + " — " + wsName
	summary := templateBodySummary(chosen)
	if summary == "" {
		summary = description
	}
	tags := []string{
		"workspace_profile",
		"template:" + strings.ToLower(strings.ReplaceAll(templateName, " ", "-")),
		strings.ToLower(wsName),
	}
	ts := time.Now().UTC().Format("2006-01-02 15:04:05")
	doc := NewMemoryDoc(id, MemoryTypeWorkspaceProfile, description, summary, tags)
	doc.FountainBody = buildProfileFountainBody(ts, templateName, chosen)
	if err := store.Save(doc, embedder); err != nil {
		return fmt.Errorf("profile use: save: %w", err)
	}
	fmt.Fprintf(out, green("✓")+" Switched to %q. Type your first message to continue.\n", templateName)

	// Step 4 — reset history so the new profile is injected on the next turn.
	a.ClearHistory()
	return nil
}

// profileSelectTemplate resolves the template to use for /profile use.
// If args[0] is provided, it attempts to load by stem name or display name.
// If not found, or if no name is given, it shows the interactive picker.
// Returns raw template bytes and the display name.
func profileSelectTemplate(a *Agent, args []string, wsRoot string, out io.Writer) ([]byte, string) {
	if len(args) > 0 {
		name := args[0]
		templates := ListTemplates(wsRoot)
		for _, t := range templates {
			stem := strings.TrimSuffix(t.File, ".fountain")
			if stem == name || strings.EqualFold(t.Name, name) {
				var data []byte
				var err error
				if t.Source == "workspace" && wsRoot != "" {
					localPath := filepath.Join(wsRoot, "agents", "templates", "profiles", t.File)
					data, err = os.ReadFile(localPath)
				}
				if data == nil || err != nil {
					data, err = LoadTemplate(t.File)
				}
				if err == nil {
					return maybeEditTemplate(data, out), t.Name
				}
			}
		}
		fmt.Fprintf(out, "  Template %q not found — showing picker.\n", name)
	}

	// Interactive picker via SelectFrom.
	templates := ListTemplates(wsRoot)
	fmt.Fprintln(out, "\nChoose a profile:")
	items := make([]SelectItem, len(templates))
	for i, t := range templates {
		label := t.Name
		if t.Recommended != "" {
			label += "\n        " + t.Recommended
		}
		items[i] = SelectItem{Value: t.Name, Label: label}
	}
	chosen, err := SelectFrom(items, fmt.Sprintf("Select [1-%d] or press Enter for Blank: ", len(items)), a.In, out)
	if err != nil {
		return nil, ""
	}
	// "" means cancelled → use Blank
	if chosen == "" {
		data, _ := LoadTemplate("blank")
		return maybeEditTemplate(data, out), "Blank"
	}
	// Find the template by name and load it.
	for _, t := range templates {
		if t.Name == chosen {
			var data []byte
			if t.Source == "workspace" && wsRoot != "" {
				localPath := filepath.Join(wsRoot, "agents", "templates", "profiles", t.File)
				data, _ = os.ReadFile(localPath)
			}
			if data == nil {
				data, _ = LoadTemplate(t.File)
			}
			if data != nil {
				return maybeEditTemplate(data, out), t.Name
			}
		}
	}
	// Template not found (shouldn't happen) → Blank.
	data, _ := LoadTemplate("blank")
	return maybeEditTemplate(data, out), "Blank"
}

// maybeEditTemplate opens the template in $EDITOR when running interactively.
// Returns the original content on error or when not interactive.
func maybeEditTemplate(content []byte, out io.Writer) []byte {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return content
	}
	edited, err := editTemplateRaw(content, out)
	if err != nil {
		fmt.Fprintf(out, yellow("  ✗")+" Editor: %v — using template as-is.\n", err)
		return content
	}
	return edited
}

// cmdMemoryProfileUpdate opens the most recent workspace_profile in $EDITOR and re-saves it.
func cmdMemoryProfileUpdate(a *Agent, out io.Writer, store *MemoryStore) error {
	metas, err := store.List(string(MemoryTypeWorkspaceProfile))
	if err != nil {
		return fmt.Errorf("profile update: %w", err)
	}
	if len(metas) == 0 {
		fmt.Fprintln(out, "No workspace_profile memories found. Start Harvey in a fresh workspace to run onboarding.")
		return nil
	}
	// List is ordered by updated_at DESC; first entry is most recent.
	doc, err := store.ByID(metas[0].ID)
	if err != nil {
		return fmt.Errorf("profile update: load doc: %w", err)
	}
	if doc == nil {
		fmt.Fprintln(out, "Profile document not found on disk.")
		return nil
	}
	edited, err := editInEditor(doc, out)
	if err != nil {
		return fmt.Errorf("profile update: editor: %w", err)
	}
	var embedder Embedder
	if entry := a.Config.Memory.ActiveRagStore(); entry != nil {
		embedder = NewEmbedderForEntry(entry, a.Config.Ollama.URL)
	}
	if err := store.Save(edited, embedder); err != nil {
		return fmt.Errorf("profile update: save: %w", err)
	}
	fmt.Fprintf(out, green("✓")+" Profile updated: %s\n", edited.Meta.ID)
	return nil
}
