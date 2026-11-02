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

)

// ── /memory ──────────────────────────────────────────────────────────────────

/** cmdMemory dispatches /memory subcommands: mine, list, show, flag, forget,
 * status, recall.
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
	noteMemoryRetiring(out)
	if len(args) == 0 {
		return Usagef("usage: /memory <mine|list|show|flag|forget|status|recall> [args...]")
	}
	if a.Memory == nil || a.Memory.Store == nil {
		return Negativef("/memory: memory store not available")
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
	default:
		return Usagef("unknown /memory subcommand: %q  (usage: /memory <mine|list|show|flag|forget|status|recall> [args...])", args[0])
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

	var embedder Embedder = a.memoryEmbedder()

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
			return Negativef("no memories found")
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
		return Negativef("memory %q not found", args[0])
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
			return Negativef("no memories found")
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
			return Negativef("no memories found")
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
		fmt.Fprintf(out, "Memory budget:   %.0f%% of context (%d tokens on %s)\n",
			budgetPct*100, a.memoryBudget(), modelName)
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
		return Usagef("usage: /memory recall <query>")
	}
	query := strings.Join(args, " ")

	var embedder Embedder = a.memoryEmbedder()

	results, err := a.Memory.Unified.Recall(query, embedder, 0)
	if err != nil {
		return fmt.Errorf("memory recall: %w", err)
	}
	if len(results) == 0 {
		fmt.Fprintln(out, "No memories found.")
		return Negativef("memory recall: no memories match %q", query)
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
