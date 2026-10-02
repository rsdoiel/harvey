// Package harvey — commands_rag.go implements the /rag slash command family
// for managing RAG stores and ingesting documents.
package harvey

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ─── /rag ────────────────────────────────────────────────────────────────────

/** cmdRag handles Retrieval-Augmented Generation (RAG) store management and
 * context injection. RAG allows Harvey to retrieve relevant document snippets
 * and inject them into the conversation context before each prompt.
 *
 * Subcommands:
 *   status    — Show active store and all registered stores
 *   list      — List all registered RAG stores
 *   on        — Enable RAG context injection for current session
 *   off       — Disable RAG context injection for current session
 *   setup     — Create a new RAG store (interactive or with defaults)
 *   new       — Create a named RAG store with interactive setup
 *   use       — Activate a different RAG store
 *   drop      — Remove a store from the registry
 *   ingest    — Ingest files/directories into the active store
 *   query     — Query the active store and show matching chunks
 *
 * RAG stores are SQLite databases bound to a specific embedding model.
 * Only the active store is kept open in memory. Each store can be queried
 * independently and switched as needed for different projects or domains.
 *
 * Parameters:
 *   a    (*Agent)    — Harvey agent with RAG configuration.
 *   args ([]string)  — Command arguments from user input.
 *   out  (io.Writer) — Destination for command output.
 *
 * Returns:
 *   error — On command execution failure.
 */
func cmdRag(a *Agent, args []string, out io.Writer) error {
	if len(args) == 0 {
		return ragStatus(a, out)
	}
	switch strings.ToLower(args[0]) {
	case "status":
		return ragStatus(a, out)
	case "list":
		return ragList(a, out)
	case "on":
		if a.Rag == nil {
			return Negativef("RAG is not configured. Run /rag new NAME first")
		}
		a.RagOn = true
		a.Config.Memory.RagEnabled = true
		fmt.Fprintln(out, "RAG context injection: on")
		if err := SaveMemoryConfig(a.Workspace, a.Config); err != nil {
			return persistFailure(out, "config", err)
		}
	case "off":
		a.RagOn = false
		a.Config.Memory.RagEnabled = false
		fmt.Fprintln(out, "RAG context injection: off")
		if err := SaveMemoryConfig(a.Workspace, a.Config); err != nil {
			return persistFailure(out, "config", err)
		}
	case "new":
		if len(args) < 2 {
			return Usagef("usage: /rag new NAME [--embedder ollama|encoderfile] [--embedder-url URL]")
		}
		kind, url := ParseEmbedderFlags(args[2:])
		return ragWizard(a, args[1], kind, url, out)
	case "use":
		if len(args) < 2 {
			items := ragStoreSelectItems(a)
			if len(items) == 0 {
				return Negativef("no RAG stores registered. Run /rag new NAME to create one")
			}
			chosen, err := SelectFrom(items, fmt.Sprintf("Select store [1-%d] or Enter to cancel: ", len(items)), a.In, out)
			if err != nil || chosen == "" {
				return err
			}
			args = append(args, chosen)
		}
		return ragSwitch(a, args[1], out)
	case "show":
		name := ""
		if len(args) >= 2 {
			name = args[1]
		}
		return ragShow(a, name, out)
	case "drop", "remove":
		if len(args) < 2 {
			items := ragStoreSelectItems(a)
			if len(items) == 0 {
				return Negativef("no RAG stores registered")
			}
			chosen, err := SelectFrom(items, fmt.Sprintf("Remove which store [1-%d] or Enter to cancel: ", len(items)), a.In, out)
			if err != nil || chosen == "" {
				return err
			}
			args = append(args, chosen)
		}
		return ragDrop(a, args[1], out)
	case "ingest":
		if len(args) < 2 {
			return Usagef("usage: /rag ingest PATH [PATH...] [--doi DOI] [--url URL] [--title TITLE] [--version VER] [--rights RIGHTS]")
		}
		return ragIngest(a, args[1:], out)
	case "query":
		if len(args) < 2 {
			return Usagef("usage: /rag query TEXT")
		}
		return ragQuery(a, strings.Join(args[1:], " "), out)
	default:
		return Usagef("unknown rag subcommand: %s", args[0])
	}
	return nil
}

// ragStatus prints the active store details and the full store registry.
func ragStatus(a *Agent, out io.Writer) error {
	enabled := "off"
	if a.RagOn {
		enabled = "on"
	}
	fmt.Fprintf(out, "RAG context injection: %s\n", enabled)

	entry := a.Config.Memory.ActiveRagStore()
	if entry == nil {
		fmt.Fprintln(out, "No store configured. Run /rag new NAME to get started.")
		return nil
	}

	fmt.Fprintf(out, "Active store:    %s\n", entry.Name)
	fmt.Fprintf(out, "  Database:      %s\n", entry.DBPath)
	fmt.Fprintf(out, "  Embed model:   %s\n", entry.EmbeddingModel)
	if entry.EmbedderKind == "encoderfile" {
		fmt.Fprintf(out, "  Embedder:      encoderfile (%s)\n", entry.EmbedderURL)
	}
	if a.Rag != nil {
		if n, err := a.Rag.Count(); err == nil {
			fmt.Fprintf(out, "  Chunks:        %d\n", n)
		}
	} else {
		fmt.Fprintln(out, "  (store not open)")
	}
	if len(entry.ModelMap) > 0 {
		fmt.Fprintln(out, "  Model map:")
		for gen, emb := range entry.ModelMap {
			fmt.Fprintf(out, "    %-36s → %s\n", gen, emb)
		}
	}

	if len(a.Config.Memory.RagStores) > 1 {
		fmt.Fprintf(out, "\nAll stores (%d):\n", len(a.Config.Memory.RagStores))
		for _, e := range a.Config.Memory.RagStores {
			marker := "  "
			if e.Name == a.Config.Memory.RagActive {
				marker = "* "
			}
			fmt.Fprintf(out, "  %s%-16s %s  (%s)\n", marker, e.Name, e.DBPath, e.EmbeddingModel)
		}
	}
	return nil
}

// ragList prints a brief listing of all registered stores.
func ragList(a *Agent, out io.Writer) error {
	if len(a.Config.Memory.RagStores) == 0 {
		fmt.Fprintln(out, "No RAG stores registered. Run /rag new NAME to create one.")
		return nil
	}
	fmt.Fprintf(out, "RAG stores (%d):\n", len(a.Config.Memory.RagStores))
	for _, e := range a.Config.Memory.RagStores {
		marker := "  "
		if e.Name == a.Config.Memory.RagActive {
			marker = "* "
		}
		fmt.Fprintf(out, "  %s%-16s %s  (%s)\n", marker, e.Name, e.DBPath, e.EmbeddingModel)
	}
	return nil
}

/** ragShow prints details for a named (or active) RAG store.
 *
 * Parameters:
 *   a    (*Agent)    — Harvey agent.
 *   name (string)   — store name; empty string selects the active store.
 *   out  (io.Writer) — output writer.
 *
 * Returns:
 *   error — always nil; errors are printed to out.
 *
 * Example:
 *   /rag show
 *   /rag show golang
 */
func ragShow(a *Agent, name string, out io.Writer) error {
	var entry *RagStoreEntry
	if name == "" {
		entry = a.Config.Memory.ActiveRagStore()
		if entry == nil {
			return Negativef("no store configured. Run /rag new NAME to create one")
		}
	} else {
		entry = a.Config.Memory.RagStoreByName(name)
		if entry == nil {
			return Negativef("store %q not found. Use /rag list to see registered stores", name)
		}
	}
	active := ""
	if entry.Name == a.Config.Memory.RagActive {
		active = " (active)"
	}
	fmt.Fprintf(out, "  Name:          %s%s\n", entry.Name, active)
	fmt.Fprintf(out, "  Database:      %s\n", entry.DBPath)
	fmt.Fprintf(out, "  Embed model:   %s\n", entry.EmbeddingModel)
	if entry.EmbedderKind == "encoderfile" {
		fmt.Fprintf(out, "  Embedder:      encoderfile (%s)\n", entry.EmbedderURL)
	}
	if entry.Name == a.Config.Memory.RagActive && a.Rag != nil {
		if n, err := a.Rag.Count(); err == nil {
			fmt.Fprintf(out, "  Chunks:        %d\n", n)
		}
	} else {
		fmt.Fprintln(out, "  Chunks:        (store not open)")
	}
	if len(entry.ModelMap) > 0 {
		fmt.Fprintln(out, "  Model map:")
		for gen, emb := range entry.ModelMap {
			fmt.Fprintf(out, "    %-36s → %s\n", gen, emb)
		}
	}
	return nil
}

// ragSwitch closes the current store and opens the named one.
func ragSwitch(a *Agent, name string, out io.Writer) error {
	entry := a.Config.Memory.RagStoreByName(name)
	if entry == nil {
		return Negativef("store %q not found. Use /rag list to see available stores", name)
	}
	if a.Rag != nil {
		_ = a.Rag.Close()
		a.Rag = nil
	}
	dbPath, err := a.Workspace.AbsPath(entry.DBPath)
	if err != nil {
		return fmt.Errorf("rag use: %w", err)
	}
	store, err := NewRagStore(dbPath, entry.EmbeddingModel)
	if err != nil {
		return fmt.Errorf("rag use: open store: %w", err)
	}
	a.Rag = store
	a.Config.Memory.RagActive = name
	if err := SaveMemoryConfig(a.Workspace, a.Config); err != nil {
		fmt.Fprintf(out, "Warning: could not persist active store: %v\n", err)
	}
	fmt.Fprintf(out, "Active store: %s (%s)\n", entry.Name, entry.DBPath)
	return nil
}

// ragDrop removes a store from the registry (does not delete the .db file).
func ragDrop(a *Agent, name string, out io.Writer) error {
	entry := a.Config.Memory.RagStoreByName(name)
	if entry == nil {
		return Negativef("store %q not found", name)
	}
	fmt.Fprintf(out, "Remove store %q from registry? The .db file will NOT be deleted.\n", name)
	fmt.Fprintf(out, "  Database: %s\n", entry.DBPath)
	fmt.Fprint(out, "Confirm? [y/N] ")
	scanner := bufio.NewScanner(a.In)
	scanner.Scan()
	if answer := strings.ToLower(strings.TrimSpace(scanner.Text())); answer != "y" && answer != "yes" {
		fmt.Fprintln(out, "Cancelled.")
		return nil
	}
	if name == a.Config.Memory.RagActive {
		if a.Rag != nil {
			_ = a.Rag.Close()
			a.Rag = nil
		}
		a.RagOn = false
		a.Config.Memory.RagActive = ""
	}
	a.Config.Memory.RemoveRagStore(name)
	if err := SaveMemoryConfig(a.Workspace, a.Config); err != nil {
		fmt.Fprintf(out, "Warning: could not persist registry: %v\n", err)
	}
	fmt.Fprintf(out, "Store %q removed. To delete the database: rm %s\n", name, entry.DBPath)
	return nil
}

/** ParseEmbedderFlags extracts --embedder and --embedder-url values from args.
 * Unrecognised tokens are silently ignored. Both values default to "".
 *
 * Parameters:
 *   args ([]string) — remaining arguments after the store name.
 *
 * Returns:
 *   kind (string) — embedder kind: "ollama", "encoderfile", or "".
 *   url  (string) — embedder base URL, or "".
 *
 * Example:
 *   kind, url := ParseEmbedderFlags([]string{"--embedder", "encoderfile", "--embedder-url", "http://localhost:8080"})
 */
func ParseEmbedderFlags(args []string) (kind, url string) {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--embedder":
			if i+1 < len(args) {
				i++
				kind = args[i]
			}
		case "--embedder-url":
			if i+1 < len(args) {
				i++
				url = args[i]
			}
		}
	}
	return kind, url
}

// ragWizard runs the interactive setup wizard for a named store, creating or
// reconfiguring it in the registry. embedderKind and embedderURL select the
// embedder backend: "" or "ollama" uses Ollama; "encoderfile" uses an
// Encoderfile binary server at embedderURL.
/** confirmEmbedder thoroughly probes preferred with a live /api/embed call and,
 * if that fails, tries the rest of candidates in order until one is confirmed.
 * If nothing confirms — the server is unreachable, or no candidate actually
 * embeds — preferred is returned unchanged with a warning printed: a failed
 * probe never blocks /rag setup, the same rule FastProbeModel's own failures
 * follow (DR-0004).
 *
 * Parameters:
 *   ctx        (context.Context) — controls each probe's HTTP request lifetime.
 *   ollamaURL  (string)          — Ollama server base URL.
 *   preferred  (string)          — the keyword-based guess to confirm first.
 *   candidates ([]string)        — every embedding-model candidate, in the
 *     order /rag setup would otherwise have trusted; preferred need not be
 *     first, and is tried first regardless of its position.
 *   out        (io.Writer)       — destination for a fallback/warning line.
 *
 * Returns:
 *   string — a candidate ThoroughProbeModel confirmed, or preferred unchanged.
 *
 * Example:
 *   embedder := confirmEmbedder(ctx, "http://localhost:11434", "nomic-embed-text",
 *       embedModels, out)
 */
func confirmEmbedder(ctx context.Context, ollamaURL, preferred string, candidates []string, out io.Writer) string {
	ordered := append([]string{preferred}, candidates...)
	tried := map[string]bool{}
	for _, name := range ordered {
		if name == "" || tried[name] {
			continue
		}
		tried[name] = true
		cap, err := ThoroughProbeModel(ctx, ollamaURL, name)
		if err == nil && cap.SupportsEmbed == CapYes {
			if name != preferred {
				fmt.Fprintf(out, "  %s does not actually embed (confirmed live); using %s instead.\n", preferred, name)
			}
			return name
		}
	}
	fmt.Fprintf(out, "  Could not confirm an embedding model live; using %s (unconfirmed).\n", preferred)
	return preferred
}

func ragWizard(a *Agent, name, embedderKind, embedderURL string, out io.Writer) error {
	ctx := context.Background()
	ragDir := filepath.Join(harveySubdir, "rag")
	dbPath := filepath.Join(ragDir, name+".db")

	// ── Encoderfile path ───────────────────────────────────────────────────────
	if embedderKind == "encoderfile" {
		if embedderURL == "" {
			return Usagef("encoderfile requires --embedder-url, e.g. --embedder-url http://localhost:8080")
		}
		if !ProbeEncoderfile(embedderURL) {
			return Unavailablef("encoderfile server not reachable at %s  (start the server: ./your-model.encoderfile serve)", embedderURL)
		}
		modelID, err := ProbeEncoderfileModel(embedderURL)
		if err != nil {
			return fmt.Errorf("rag wizard: %w", err)
		}
		fmt.Fprintf(out, "Proposed RAG store %q (Encoderfile embedder: %s):\n\n", name, modelID)
		fmt.Fprintf(out, "  Database:      %s\n", dbPath)
		fmt.Fprintf(out, "  Embedder URL:  %s\n", embedderURL)
		fmt.Fprintf(out, "  Model ID:      %s\n\n", modelID)
		fmt.Fprint(out, "Accept? [Y/n] ")

		scanner := bufio.NewScanner(a.In)
		scanner.Scan()
		answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
		if answer != "" && answer != "y" && answer != "yes" {
			fmt.Fprintln(out, "Setup cancelled.")
			return nil
		}
		if err := a.Workspace.MkdirAll(ragDir); err != nil {
			return fmt.Errorf("rag setup: create directory: %w", err)
		}
		entry := RagStoreEntry{
			Name:           name,
			DBPath:         dbPath,
			EmbeddingModel: modelID,
			EmbedderKind:   "encoderfile",
			EmbedderURL:    embedderURL,
		}
		return ragCommitEntry(a, entry, out)
	}

	// ── Ollama path ────────────────────────────────────────────────────────────
	if !ProbeOllama(a.Config.Ollama.URL) {
		fmt.Fprintln(out, "Ollama is not running. Start it in a shell with: ollama serve")
		return nil
	}

	// Step 0: detect available embedding models via the model cache.
	var embedModels []string
	if a.ModelCache != nil {
		all, err := a.ModelCache.All()
		if err == nil {
			for _, c := range all {
				if c.SupportsEmbed == CapYes {
					embedModels = append(embedModels, c.Name)
				}
			}
		}
	}

	// If cache is empty or no embedding models found, fall back to live detection.
	if len(embedModels) == 0 {
		summaries, err := NewOllamaClient(a.Config.Ollama.URL, "").ModelSummaries(ctx)
		if err == nil {
			for _, s := range summaries {
				if hasEmbedKeyword(s.Name) {
					embedModels = append(embedModels, s.Name)
				}
			}
		}
	}

	if len(embedModels) == 0 {
		fmt.Fprintln(out, "No embedding models found on this Ollama server.")
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "Recommended options (in a shell, run: ollama pull NAME):")
		fmt.Fprintln(out, "  nomic-embed-text        (~274 MB) — best general-purpose retrieval")
		fmt.Fprintln(out, "  mxbai-embed-large       (~670 MB) — high quality retrieval")
		fmt.Fprintln(out, "  qllama/bge-small-en-v1.5 (~46 MB) — small but retrieval-optimized")
		fmt.Fprintln(out, "  bge-m3                  (~1.2 GB) — multilingual (good for SEA-LION)")
		fmt.Fprintln(out, "  (avoid all-minilm — it is similarity-tuned, not retrieval-tuned)")
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "After pulling an embedding model, run /rag new NAME again.")
		fmt.Fprintln(out, "Or use an Encoderfile binary: /rag new NAME --embedder encoderfile --embedder-url URL")
		return nil
	}

	// Pick preferred embedding model in quality order; all are retrieval-optimized
	// except all-minilm (similarity-only) which is the last resort.
	preferred := embedModels[0]
	for _, pref := range []string{"nomic-embed-text", "mxbai-embed-large", "bge-m3", "bge-", "gte-", "e5-", "jina", "all-minilm"} {
		for _, m := range embedModels {
			if strings.Contains(strings.ToLower(m), pref) {
				preferred = m
				goto foundPref
			}
		}
	}
foundPref:
	// The name-based guess above is confirmed (or corrected) with one live
	// /api/embed call before committing to it — everything else in embedModels
	// was itself only a keyword guess, and a wrong one here silently degrades
	// every future RAG query, unlike a wrong guess elsewhere in the model cache.
	preferred = confirmEmbedder(ctx, a.Config.Ollama.URL, preferred, embedModels, out)

	// Build proposed model map: all non-embedding generation models → preferred embedder.
	genModels, _ := newOllamaLLMClient(a.Config.Ollama.URL, "", a.Config.Ollama.Timeout).Models(ctx)
	proposed := make(map[string]string)
	for _, m := range genModels {
		if !hasEmbedKeyword(m) {
			embedFor := preferred
			// Multilingual hint: suggest bge-m3 for models with multilingual signals.
			lower := strings.ToLower(m)
			if strings.Contains(lower, "sea") || strings.Contains(lower, "lion") ||
				strings.Contains(lower, "multilingual") || strings.Contains(lower, "multi") {
				for _, em := range embedModels {
					if strings.Contains(strings.ToLower(em), "bge-m3") {
						embedFor = em
						break
					}
				}
			}
			proposed[m] = embedFor
		}
	}

	// Display proposed mapping for human review.
	fmt.Fprintf(out, "Proposed RAG store %q (embedding model: %s):\n\n", name, preferred)
	fmt.Fprintf(out, "  Database: %s\n\n", dbPath)
	if len(proposed) > 0 {
		fmt.Fprintf(out, "  %-36s  %s\n", "Generation model", "Embedding model")
		fmt.Fprintf(out, "  %s  %s\n", strings.Repeat("─", 36), strings.Repeat("─", 24))
		for gen, emb := range proposed {
			fmt.Fprintf(out, "  %-36s  %s\n", ollamaTruncateName(gen, 36), emb)
		}
	}
	fmt.Fprintln(out, "")
	fmt.Fprint(out, "Accept? [Y/n] ")

	scanner := bufio.NewScanner(a.In)
	scanner.Scan()
	answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
	if answer != "" && answer != "y" && answer != "yes" {
		fmt.Fprintln(out, "Setup cancelled.")
		return nil
	}

	// Ensure the rag/ subdirectory exists.
	if err := a.Workspace.MkdirAll(ragDir); err != nil {
		return fmt.Errorf("rag setup: create directory: %w", err)
	}

	entry := RagStoreEntry{
		Name:           name,
		DBPath:         dbPath,
		EmbeddingModel: preferred,
		ModelMap:       proposed,
	}
	return ragCommitEntry(a, entry, out)
}

// ragCommitEntry persists entry as the active RAG store, opens its database,
// and enables RAG injection. It is called by both the Ollama and Encoderfile
// wizard paths.
func ragCommitEntry(a *Agent, entry RagStoreEntry, out io.Writer) error {
	a.Config.Memory.AddOrUpdateRagStore(entry)
	a.Config.Memory.RagActive = entry.Name
	a.Config.Memory.RagEnabled = true

	if err := SaveMemoryConfig(a.Workspace, a.Config); err != nil {
		return fmt.Errorf("rag setup: save config: %w", err)
	}

	// Close any previously open store, then open the new one.
	if a.Rag != nil {
		_ = a.Rag.Close()
		a.Rag = nil
	}
	absDB, err := a.Workspace.AbsPath(entry.DBPath)
	if err != nil {
		return err
	}
	store, err := NewRagStore(absDB, entry.EmbeddingModel)
	if err != nil {
		return fmt.Errorf("rag setup: open store: %w", err)
	}
	a.Rag = store
	a.RagOn = true

	fmt.Fprintf(out, "RAG store %q configured and enabled.\n", entry.Name)
	fmt.Fprintf(out, "Next step: run /rag ingest <file-or-directory> to populate the store.\n")
	return nil
}

// ragIngest chunks and embeds each path into the RAG store.
// ragLargeFileThreshold is the file size above which /rag ingest shows the
// document list and asks for confirmation before starting.
const ragLargeFileThreshold = 1000 * 1024 // 1000 KB

// ragIngestableExts is the set of file extensions eligible for RAG ingestion.
var ragIngestableExts = map[string]bool{
	".md": true, ".txt": true, ".go": true, ".ts": true, ".js": true, ".css": true, ".py": true,
	".Mod": true, ".obn": true, ".pas": true, ".lisp": true, ".bas": true, ".c": true, ".cpp": true,
	".rs": true, ".yaml": true, ".yml": true, ".toml": true, ".sql": true, ".pdf": true,
}

// ragCollectFiles expands a list of paths (files and directories) into the
// ordered list of absolute file paths that ragIngest would process.
func ragCollectFiles(paths []string, absPathFn func(string) (string, error)) ([]string, error) {
	var files []string
	for _, p := range paths {
		abs, err := absPathFn(p)
		if err != nil {
			return nil, fmt.Errorf("collect %s: %w", p, err)
		}
		info, err := os.Stat(abs)
		if err != nil {
			return nil, fmt.Errorf("collect %s: %w", p, err)
		}
		if info.IsDir() {
			err := filepath.WalkDir(abs, func(path string, d fs.DirEntry, werr error) error {
				if werr != nil || d.IsDir() {
					return werr
				}
				if d.Type()&fs.ModeSymlink != 0 {
					return nil
				}
				if ragIngestableExts[strings.ToLower(filepath.Ext(path))] {
					files = append(files, path)
				}
				return nil
			})
			if err != nil {
				return nil, err
			}
		} else if ragIngestableExts[strings.ToLower(filepath.Ext(abs))] {
			files = append(files, abs)
		}
	}
	return files, nil
}

// ragCountLarge returns the number of files whose size exceeds ragLargeFileThreshold.
func ragCountLarge(files []string) int {
	n := 0
	for _, f := range files {
		info, err := os.Stat(f)
		if err == nil && info.Size() > ragLargeFileThreshold {
			n++
		}
	}
	return n
}

func ragIngest(a *Agent, paths []string, out io.Writer) error {
	if a.Rag == nil {
		return Negativef("RAG is not configured. Run /rag new NAME first")
	}
	entry := a.Config.Memory.ActiveRagStore()
	if entry == nil {
		return Negativef("no active RAG store. Run /rag use NAME to select one")
	}
	embedder := NewEmbedderForEntry(entry, a.Config.Ollama.URL)

	// Parse provenance flags and separate them from file paths.
	var meta ProvenanceMeta
	var rawPaths []string
	for i := 0; i < len(paths); i++ {
		switch paths[i] {
		case "--doi":
			if i+1 < len(paths) {
				i++
				meta.DOI = paths[i]
			}
		case "--url":
			if i+1 < len(paths) {
				i++
				meta.URL = paths[i]
			}
		case "--title":
			if i+1 < len(paths) {
				i++
				meta.Title = paths[i]
			}
		case "--version":
			if i+1 < len(paths) {
				i++
				meta.Version = paths[i]
			}
		case "--rights":
			if i+1 < len(paths) {
				i++
				meta.Rights = paths[i]
			}
		default:
			rawPaths = append(rawPaths, paths[i])
		}
	}

	// Separate remote URIs from local paths. Remote S3 prefixes are ingested
	// directly (download → ingest → remove per object) without the large-file
	// confirmation flow, since the user explicitly addressed them by URI.
	var failed error // first failure from any source; every source is still tried
	var localPaths []string
	for _, p := range rawPaths {
		switch parseURIScheme(p) {
		case "":
			localPaths = append(localPaths, p)
		case "s3":
			failed = keepFirstFailure(failed, ragIngestS3Prefix(a, p, embedder, out))
		case "sftp", "scp":
			r, err := NewRemoteReader(p)
			if err != nil {
				fmt.Fprintf(out, "  ✗ %s: %v\n", p, err)
				failed = keepFirstFailure(failed, defaultClass(ClassUsage, fmt.Errorf("%s: %w", p, err)))
				continue
			}
			failed = keepFirstFailure(failed, ragIngestRemotePrefix(a, r, parseURIScheme(p), p, embedder, out))
		case "http", "https":
			failed = keepFirstFailure(failed, ragIngestHTTP(a, p, embedder, out))
		default:
			fmt.Fprintf(out, "  ⚠ unsupported scheme %q, skipping %s\n", parseURIScheme(p), p)
			failed = keepFirstFailure(failed, Usagef("unsupported scheme %q in %s", parseURIScheme(p), p))
		}
	}
	if len(localPaths) == 0 {
		return failed
	}

	// Collect all candidate files across all given local paths.
	files, err := ragCollectFiles(localPaths, a.Workspace.AbsPath)
	if err != nil {
		return keepFirstFailure(failed, defaultClass(ClassNoInput, fmt.Errorf("collecting files: %w", err)))
	}
	if len(files) == 0 {
		return keepFirstFailure(failed, Negativef("no ingestable files found"))
	}

	// When there are multiple files or any file is large, show the list and
	// ask for confirmation before starting the embedding work.
	largeCount := ragCountLarge(files)
	if len(files) > 1 || largeCount > 0 {
		fmt.Fprintf(out, "Files to ingest (%d):\n", len(files))
		for _, f := range files {
			info, err := os.Stat(f)
			sizeNote := ""
			if err == nil && info.Size() > ragLargeFileThreshold {
				sizeNote = fmt.Sprintf("  [%.0f KB]", float64(info.Size())/1024)
			}
			fmt.Fprintf(out, "  %s%s\n", f, sizeNote)
		}
		if largeCount > 0 {
			fmt.Fprintf(out, "\nNote: %d file(s) exceed 100 KB and may take longer to embed.\n", largeCount)
		}
		fmt.Fprint(out, "Proceed? [y/N] ")
		scanner := bufio.NewScanner(a.In)
		scanner.Scan()
		if answer := strings.ToLower(strings.TrimSpace(scanner.Text())); answer != "y" && answer != "yes" {
			fmt.Fprintln(out, "Cancelled.")
			return failed
		}
	}

	// Ingest each file individually, reporting progress.
	var total int
	for i, absFile := range files {
		fmt.Fprintf(out, "  [%d/%d] %s", i+1, len(files), absFile)
		if strings.ToLower(filepath.Ext(absFile)) == ".pdf" {
			n, diagrams, err := ragIngestPDF(a.Rag, embedder, absFile, meta)
			if err != nil {
				fmt.Fprintf(out, " — error: %v\n", err)
				failed = keepFirstFailure(failed, defaultClass(ClassUnavailable, fmt.Errorf("%s: %w", absFile, err)))
			} else {
				fmt.Fprintf(out, " — %d chunk(s)", n)
				if len(diagrams) > 0 {
					fmt.Fprintf(out, " (%d diagram-only page(s) flagged)", len(diagrams))
				}
				fmt.Fprintln(out)
				total += n
			}
		} else {
			n, err := ragIngestFile(a.Rag, embedder, absFile, meta)
			if err != nil {
				fmt.Fprintf(out, " — error: %v\n", err)
				failed = keepFirstFailure(failed, defaultClass(ClassUnavailable, fmt.Errorf("%s: %w", absFile, err)))
			} else {
				fmt.Fprintf(out, " — %d chunk(s)\n", n)
				total += n
			}
		}
	}
	fmt.Fprintf(out, "Ingested %d chunk(s) total from %d file(s).\n", total, len(files))
	return failed
}

/** ragIngestRemotePrefix lists all ingestable objects under a remote prefix URI
 * using r and ingests each by downloading to a temp file, ingesting, then
 * deleting the temp file immediately. This keeps peak disk usage bounded to a
 * single object regardless of prefix size.
 *
 * Works for any RemoteReader that implements List (s3://, sftp://, scp://).
 * HTTP/HTTPS callers should use ragIngestHTTP instead, as HTTP has no directory
 * listing protocol.
 *
 * Parameters:
 *   a       (*Agent)       — Harvey agent; a.Rag must be non-nil.
 *   r       (RemoteReader) — backend to use for List and Get.
 *   scheme  (string)       — URI scheme ("s3", "sftp", "scp"); used in progress messages.
 *   uri     (string)       — prefix URI to list and ingest.
 *   embedder (Embedder)    — embedding model for the active store.
 *   out     (io.Writer)    — progress and error output.
 *
 * Example:
 *   r, _ := NewRemoteReader("sftp://host/docs/")
 *   ragIngestRemotePrefix(a, r, "sftp", "sftp://host/docs/", embedder, out)
 */
func ragIngestRemotePrefix(a *Agent, r RemoteReader, scheme, uri string, embedder Embedder, out io.Writer) error {
	objects, err := r.List(context.Background(), uri)
	if err != nil {
		fmt.Fprintf(out, "  ✗ list %s: %v\n", uri, err)
		return defaultClass(ClassUnavailable, fmt.Errorf("list %s: %w", uri, err))
	}
	var ingested int
	var failed error // first failure; every object is still tried
	for _, obj := range objects {
		if obj.IsDir {
			continue
		}
		ext := strings.ToLower(filepath.Ext(obj.URI))
		if !ragIngestableExts[ext] {
			continue
		}
		f, err := os.CreateTemp("", "harvey-"+scheme+"-*"+ext)
		if err != nil {
			fmt.Fprintf(out, "  ✗ temp for %s: %v\n", obj.URI, err)
			failed = keepFirstFailure(failed, AsCreate(fmt.Errorf("temp for %s: %w", obj.URI, err)))
			continue
		}
		tmpPath := f.Name()
		if err := r.Get(context.Background(), obj.URI, f); err != nil {
			f.Close()
			os.Remove(tmpPath)
			fmt.Fprintf(out, "  ✗ download %s: %v\n", obj.URI, err)
			failed = keepFirstFailure(failed, defaultClass(ClassUnavailable, fmt.Errorf("download %s: %w", obj.URI, err)))
			continue
		}
		f.Close()

		fmt.Fprintf(out, "  %s", obj.URI)
		var n int
		if ext == ".pdf" {
			var diagrams []int
			n, diagrams, err = ragIngestPDF(a.Rag, embedder, tmpPath, ProvenanceMeta{})
			if err != nil {
				fmt.Fprintf(out, " — error: %v\n", err)
				failed = keepFirstFailure(failed, defaultClass(ClassUnavailable, fmt.Errorf("%s: %w", obj.URI, err)))
			} else {
				fmt.Fprintf(out, " — %d chunk(s)", n)
				if len(diagrams) > 0 {
					fmt.Fprintf(out, " (%d diagram-only page(s) flagged)", len(diagrams))
				}
				fmt.Fprintln(out)
				ingested += n
			}
		} else {
			n, err = ragIngestFile(a.Rag, embedder, tmpPath, ProvenanceMeta{})
			if err != nil {
				fmt.Fprintf(out, " — error: %v\n", err)
				failed = keepFirstFailure(failed, defaultClass(ClassUnavailable, fmt.Errorf("%s: %w", obj.URI, err)))
			} else {
				fmt.Fprintf(out, " — %d chunk(s)\n", n)
				ingested += n
			}
		}
		os.Remove(tmpPath) // immediate cleanup regardless of ingest outcome
	}
	if ingested > 0 {
		fmt.Fprintf(out, "  %s: ingested %d chunk(s) from %s\n", scheme, ingested, uri)
	}
	return failed
}

// ragIngestS3Prefix is kept for backwards compatibility; delegates to ragIngestRemotePrefix.
func ragIngestS3Prefix(a *Agent, uri string, embedder Embedder, out io.Writer) error {
	s3r, err := newS3Reader()
	if err != nil {
		fmt.Fprintf(out, "  ✗ %s: %v\n", uri, err)
		return defaultClass(ClassUnavailable, fmt.Errorf("%s: %w", uri, err))
	}
	return ragIngestRemotePrefix(a, s3r, "s3", uri, embedder, out)
}

/** ragIngestHTTP downloads a single HTTP or HTTPS resource, writes it to a
 * temp file, ingests it into the active RAG store, then deletes the temp file.
 *
 * Parameters:
 *   a        (*Agent)    — Harvey agent; a.Rag must be non-nil.
 *   uri      (string)    — http:// or https:// URL of the resource.
 *   embedder (Embedder)  — embedding model for the active store.
 *   out      (io.Writer) — progress and error output.
 *
 * Example:
 *   ragIngestHTTP(a, "https://example.com/spec.md", embedder, out)
 */
func ragIngestHTTP(a *Agent, uri string, embedder Embedder, out io.Writer) error {
	ext := strings.ToLower(filepath.Ext(uri))
	if ext == "" {
		ext = ".txt" // treat extensionless URLs as plain text
	}
	f, err := os.CreateTemp("", "harvey-http-*"+ext)
	if err != nil {
		fmt.Fprintf(out, "  ✗ temp for %s: %v\n", uri, err)
		return AsCreate(fmt.Errorf("temp for %s: %w", uri, err))
	}
	tmpPath := f.Name()
	r := newHTTPReader()
	if err := r.Get(context.Background(), uri, f); err != nil {
		f.Close()
		os.Remove(tmpPath)
		fmt.Fprintf(out, "  ✗ %s: %v\n", uri, err)
		return defaultClass(ClassUnavailable, fmt.Errorf("%s: %w", uri, err))
	}
	f.Close()
	defer os.Remove(tmpPath)

	fmt.Fprintf(out, "  %s", uri)
	var n int
	if ext == ".pdf" {
		var diagrams []int
		n, diagrams, err = ragIngestPDF(a.Rag, embedder, tmpPath, ProvenanceMeta{})
		if err != nil {
			fmt.Fprintf(out, " — error: %v\n", err)
			return defaultClass(ClassUnavailable, fmt.Errorf("%s: %w", uri, err))
		}
		fmt.Fprintf(out, " — %d chunk(s)", n)
		if len(diagrams) > 0 {
			fmt.Fprintf(out, " (%d diagram-only page(s) flagged)", len(diagrams))
		}
		fmt.Fprintln(out)
	} else {
		n, err = ragIngestFile(a.Rag, embedder, tmpPath, ProvenanceMeta{})
		if err != nil {
			fmt.Fprintf(out, " — error: %v\n", err)
			return defaultClass(ClassUnavailable, fmt.Errorf("%s: %w", uri, err))
		}
		fmt.Fprintf(out, " — %d chunk(s)\n", n)
	}
	if n > 0 {
		fmt.Fprintf(out, "  http: ingested %d chunk(s) from %s\n", n, uri)
	}
	return nil
}

// ragIngestFile reads a file, splits it into chunks (language-aware when a
// chunker is registered, paragraph-based otherwise), and ingests them.
// Binary files are silently skipped (returns 0, nil).
func ragIngestFile(store *RagStore, embedder Embedder, path string, meta ProvenanceMeta) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}

	// Skip binary files silently.
	if !isTextContent(data) {
		return 0, nil
	}

	// Use filepath.Ext directly (case-preserving) so .Mod → oberon and .mod → go.
	ext := filepath.Ext(path)
	langID, _ := globalRegistry.DetectFromExtension(ext)

	var enriched []EnrichedChunk
	if langID != "" {
		if chunker := globalRegistry.GetChunker(langID); chunker != nil {
			enriched = chunker.Chunk(string(data), path)
		}
	}

	// Fallback: generic paragraph chunking wrapped as EnrichedChunks.
	if len(enriched) == 0 {
		for _, text := range ragChunk(string(data)) {
			enriched = append(enriched, EnrichedChunk{
				Content:   text,
				ChunkType: "code",
			})
		}
	}

	if len(enriched) == 0 {
		return 0, nil
	}

	// Populate Docs fields using documentation extractor when available.
	if langID != "" {
		if extractor := globalRegistry.GetExtractor(langID); extractor != nil {
			symDocs := extractor.ExtractSymbols(string(data))
			for i := range enriched {
				if len(enriched[i].Symbols) > 0 {
					if doc, ok := symDocs[enriched[i].Symbols[0]]; ok {
						enriched[i].Docs = doc
					}
				}
			}
		}
	}

	if err := store.IngestEnriched(path, enriched, embedder, meta); err != nil {
		return 0, err
	}
	return len(enriched), nil
}

// ragIngestPDF extracts text from a PDF with pdfExtract and ingests it into
// store. If the document looks like a scholarly paper (isPaperLike), it is
// chunked by section via scholarlyChunk and ingested with IngestEnriched so
// each chunk carries its section type and the document's scholarly
// identifiers/citations. Otherwise it falls back to flat per-page chunks,
// each prefixed with the document title and page number so retrieved context
// always carries its provenance. Diagram-only pages (sparse text, no raster
// images) are stored with an incomplete-content marker so retrieval results
// can surface the caveat.
// Returns (chunkCount, diagramPageNumbers, error).
func ragIngestPDF(store *RagStore, embedder Embedder, path string, meta ProvenanceMeta) (int, []int, error) {
	result, err := pdfExtract(path, "")
	if err != nil {
		return 0, nil, err
	}

	title := filepath.Base(path)
	if result.Info.Title != "" {
		title = result.Info.Title
	}

	diagramSet := make(map[int]bool, len(result.DiagramPages))
	for _, p := range result.DiagramPages {
		diagramSet[p] = true
	}

	pageTexts := strings.Split(result.Text, "\f")
	for len(pageTexts) > 0 && strings.TrimSpace(pageTexts[len(pageTexts)-1]) == "" {
		pageTexts = pageTexts[:len(pageTexts)-1]
	}

	if isPaperLike(pageTexts) {
		chunks := scholarlyChunk(pageTexts, title, diagramSet)
		if len(chunks) == 0 {
			return 0, result.DiagramPages, nil
		}
		if err := store.IngestEnriched(path, chunks, embedder, meta); err != nil {
			return 0, nil, err
		}
		return len(chunks), result.DiagramPages, nil
	}

	totalPages := result.Info.Pages
	if totalPages == 0 {
		totalPages = len(pageTexts)
	}

	var allChunks []string
	for i, pageText := range pageTexts {
		pageNum := i + 1
		isDiagram := diagramSet[pageNum]

		header := fmt.Sprintf("[PDF: %q, page %d of %d]", title, pageNum, totalPages)
		if isDiagram {
			header += diagramPageWarning
		}

		chunks := ragChunk(pageText)
		if len(chunks) == 0 {
			if isDiagram {
				// Store a placeholder so the diagram page is retrievable.
				allChunks = append(allChunks, header)
			}
			continue
		}
		for _, chunk := range chunks {
			allChunks = append(allChunks, header+"\n\n"+chunk)
		}
	}

	if len(allChunks) == 0 {
		return 0, result.DiagramPages, nil
	}
	if err := store.Ingest(path, allChunks, embedder, meta); err != nil {
		return 0, nil, err
	}
	return len(allChunks), result.DiagramPages, nil
}


// ragQuery runs a manual retrieval test against the RAG store.
func ragQuery(a *Agent, query string, out io.Writer) error {
	if a.Rag == nil {
		return Negativef("RAG is not configured. Run /rag new NAME first")
	}
	entry := a.Config.Memory.ActiveRagStore()
	if entry == nil {
		return Negativef("no active RAG store. Run /rag use NAME to select one")
	}
	embedder := NewEmbedderForEntry(entry, a.Config.Ollama.URL)
	chunks, err := a.Rag.Query(query, embedder, 5)
	if err != nil {
		return fmt.Errorf("rag query: %w", err)
	}
	if len(chunks) == 0 {
		fmt.Fprintln(out, "No results. The store may be empty — run /rag ingest first.")
		return Negativef("rag query: no results")
	}
	fmt.Fprintf(out, "Top %d result(s) for %q:\n\n", len(chunks), query)
	for i, c := range chunks {
		preview := c.Content
		if len([]rune(preview)) > 120 {
			preview = string([]rune(preview)[:119]) + "…"
		}
		if c.Source != "" {
			fmt.Fprintf(out, "  [%d] score=%.3f  source=%s\n      %s\n\n", i+1, c.Score, c.Source, preview)
		} else {
			fmt.Fprintf(out, "  [%d] score=%.3f  %s\n\n", i+1, c.Score, preview)
		}
	}
	return nil
}

