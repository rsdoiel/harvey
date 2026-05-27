package harvey

import (
	"fmt"
	"strings"
)

// factualTypes are always included at score 1.0 regardless of query.
var factualTypes = map[MemoryType]bool{
	MemoryTypeWorkspaceProfile: true,
	MemoryTypeProjectFact:      true,
}

/** UnifiedResult is one retrieved item from any memory silo.
 *
 * Fields:
 *   Source  (string)  — origin silo: "workspace_profile", "project_fact",
 *                       "experiential", "rag", or "kb".
 *   ID      (string)  — identifier within the silo (memory ID, chunk ID, etc.).
 *   Content (string)  — text to inject into the context window.
 *   Score   (float64) — similarity or relevance score; 1.0 for factual types.
 *   Tokens  (int)     — estimated token count for Content (set by Recall).
 *
 * Example:
 *   results, err := um.Recall("git error", embedder, 512)
 *   for _, r := range results {
 *       fmt.Printf("[%s %.2f] %s\n", r.Source, r.Score, r.Content)
 *   }
 */
type UnifiedResult struct {
	Source  string
	ID      string
	Content string
	Score   float64
	Tokens  int
}

/** UnifiedMemory provides budget-aware retrieval across all memory silos:
 * workspace_profile and project_fact (factual, always first), experiential
 * memories (FTS5 + optional cosine similarity), RAG chunks, and KB
 * observations.
 *
 * Example:
 *   um := NewUnifiedMemory(store, &cfg.Memory, ws)
 *   results, err := um.Recall("git error", embedder, 512)
 *   agent.AddMessage("user", FormatContext(results))
 */
type UnifiedMemory struct {
	store *MemoryStore
	cfg   *MemoryConfig
	ws    *Workspace
}

/** NewUnifiedMemory creates a UnifiedMemory instance backed by the given store.
 *
 * Parameters:
 *   store (*MemoryStore)  — open memory store.
 *   cfg   (*MemoryConfig) — memory configuration for RAG and KB access.
 *   ws    (*Workspace)    — workspace for resolving database paths; may be nil
 *                          to disable RAG and KB retrieval.
 *
 * Returns:
 *   *UnifiedMemory — ready to call Recall.
 *
 * Example:
 *   um := NewUnifiedMemory(store, &cfg.Memory, ws)
 *   results, _ := um.Recall("context query", nil, 512)
 */
func NewUnifiedMemory(store *MemoryStore, cfg *MemoryConfig, ws *Workspace) *UnifiedMemory {
	return &UnifiedMemory{store: store, cfg: cfg, ws: ws}
}

/** Recall retrieves memories from all silos in priority order, stopping when
 * the token budget is exhausted. Factual types (workspace_profile,
 * project_fact) always appear first with score 1.0. Experiential types are
 * ranked by FTS5 text match and optionally by cosine similarity. RAG chunks
 * and KB observations are appended last if budget permits.
 *
 * Parameters:
 *   query    (string)   — search text; may be empty to return factual-only.
 *   embedder (Embedder) — cosine similarity embedder; nil disables the slow
 *                         path for experiential memories and RAG.
 *   budget   (int)      — maximum estimated tokens to return; 0 means no limit.
 *
 * Returns:
 *   []UnifiedResult — results in priority order, each within the budget.
 *   error           — on store or retrieval failure.
 *
 * Example:
 *   results, err := um.Recall("git error", embedder, 512)
 *   fmt.Println(FormatContext(results))
 */
func (u *UnifiedMemory) Recall(query string, embedder Embedder, budget int) ([]UnifiedResult, error) {
	var results []UnifiedResult
	used := 0

	add := func(r UnifiedResult) bool {
		r.Tokens = estimateTokens(r.Content)
		if budget > 0 && used+r.Tokens > budget {
			return false
		}
		results = append(results, r)
		used += r.Tokens
		return true
	}

	// 1. Factual types — always first at score 1.0.
	for _, ft := range []MemoryType{MemoryTypeWorkspaceProfile, MemoryTypeProjectFact} {
		docs, err := u.store.ListDocs(string(ft))
		if err != nil {
			continue
		}
		for _, doc := range docs {
			if !add(UnifiedResult{
				Source:  string(ft),
				ID:      doc.Meta.ID,
				Content: factualContent(doc),
				Score:   1.0,
			}) {
				return results, nil
			}
		}
	}

	// 2. Experiential types — FTS5 fast path, optional cosine supplement.
	experiential, _ := u.recallExperiential(query, embedder)
	for _, r := range experiential {
		if !add(r) {
			return results, nil
		}
	}

	// 3. RAG chunks.
	if u.cfg.RagEnabled && query != "" && u.ws != nil {
		ragResults, err := u.recallRAG(query, embedder)
		if err == nil {
			for _, r := range ragResults {
				if !add(r) {
					return results, nil
				}
			}
		}
	}

	// 4. KB observations.
	if u.cfg.KnowledgeDB != "" && u.cfg.CurrentProjectID > 0 && query != "" && u.ws != nil {
		kbResults, _ := u.recallKB(query)
		for _, r := range kbResults {
			if !add(r) {
				return results, nil
			}
		}
	}

	return results, nil
}

// factualContent returns the injection text for a factual memory document.
func factualContent(doc MemoryDoc) string {
	if doc.Meta.Summary != "" {
		return doc.Meta.Description + "\n" + doc.Meta.Summary
	}
	return doc.Meta.Description
}

// recallExperiential returns non-factual memories ranked by FTS5 and
// supplemented by cosine similarity. Deduplicates by ID.
func (u *UnifiedMemory) recallExperiential(query string, embedder Embedder) ([]UnifiedResult, error) {
	topK := u.cfg.TopK
	if topK <= 0 {
		topK = 5
	}

	seen := make(map[string]bool)
	var out []UnifiedResult

	if query != "" {
		ftsResults, _ := u.store.SearchFTS(query, topK*2)
		for _, sr := range ftsResults {
			if factualTypes[sr.Doc.Meta.Type] {
				continue
			}
			if seen[sr.Doc.Meta.ID] {
				continue
			}
			seen[sr.Doc.Meta.ID] = true
			out = append(out, UnifiedResult{
				Source:  "experiential",
				ID:      sr.Doc.Meta.ID,
				Content: experientialContent(sr.Doc),
				Score:   sr.Score,
			})
			if len(out) >= topK {
				return out, nil
			}
		}
	}

	// Cosine supplement — fills remaining slots without an extra Ollama call
	// when FTS already found topK results.
	if embedder != nil && query != "" && len(out) < topK {
		cosDocs, err := u.store.Query(query, embedder, topK*2)
		if err == nil {
			for _, doc := range cosDocs {
				if factualTypes[doc.Meta.Type] {
					continue
				}
				if seen[doc.Meta.ID] {
					continue
				}
				seen[doc.Meta.ID] = true
				out = append(out, UnifiedResult{
					Source:  "experiential",
					ID:      doc.Meta.ID,
					Content: experientialContent(doc),
					Score:   0.0,
				})
				if len(out) >= topK {
					break
				}
			}
		}
	}

	return out, nil
}

// experientialContent returns the injection text for an experiential memory.
func experientialContent(doc MemoryDoc) string {
	if doc.Meta.Summary != "" {
		return string(doc.Meta.Type) + ": " + doc.Meta.Description + ". " + doc.Meta.Summary
	}
	return string(doc.Meta.Type) + ": " + doc.Meta.Description
}

// recallRAG queries the active RAG store using the provided embedder.
func (u *UnifiedMemory) recallRAG(query string, embedder Embedder) ([]UnifiedResult, error) {
	entry := u.cfg.ActiveRagStore()
	if entry == nil || embedder == nil {
		return nil, nil
	}
	topK := u.cfg.TopK
	if topK <= 0 {
		topK = 5
	}
	dbPath, err := u.ws.AbsPath(entry.DBPath)
	if err != nil {
		return nil, err
	}
	ragStore, err := NewRagStore(dbPath, entry.EmbeddingModel)
	if err != nil {
		return nil, err
	}
	defer ragStore.Close()

	chunks, err := ragStore.Query(query, embedder, topK)
	if err != nil {
		return nil, err
	}
	var out []UnifiedResult
	for _, c := range chunks {
		out = append(out, UnifiedResult{
			Source:  "rag",
			ID:      fmt.Sprintf("rag:%d", c.ID),
			Content: c.Content,
			Score:   c.Score,
		})
	}
	return out, nil
}

// recallKB returns observations from the knowledge base that contain query text.
func (u *UnifiedMemory) recallKB(query string) ([]UnifiedResult, error) {
	kb, err := OpenKnowledgeBase(u.ws, u.cfg.KnowledgeDB)
	if err != nil {
		return nil, err
	}
	defer kb.Close()

	obs, err := kb.Observations(u.cfg.CurrentProjectID)
	if err != nil {
		return nil, err
	}
	qLower := strings.ToLower(query)
	var out []UnifiedResult
	for _, o := range obs {
		if qLower != "" && !strings.Contains(strings.ToLower(o.Body), qLower) {
			continue
		}
		out = append(out, UnifiedResult{
			Source:  "kb",
			ID:      fmt.Sprintf("kb:%d", o.ID),
			Content: o.Kind + ": " + o.Body,
			Score:   0.5,
		})
		if len(out) >= 5 {
			break
		}
	}
	return out, nil
}

/** FormatContext formats a slice of UnifiedResults into a context injection
 * string grouped by source silo. Returns an empty string when results is empty.
 *
 * Parameters:
 *   results ([]UnifiedResult) — results from Recall.
 *
 * Returns:
 *   string — formatted context block ready to inject as a user message;
 *            empty string when results is empty.
 *
 * Example:
 *   block := FormatContext(results)
 *   if block != "" { agent.AddMessage("user", block) }
 */
func FormatContext(results []UnifiedResult) string {
	if len(results) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("[memory context]")
	curSource := ""
	for _, r := range results {
		if r.Source != curSource {
			sb.WriteString("\n\n[")
			sb.WriteString(sourceHeader(r.Source))
			sb.WriteString("]\n")
			curSource = r.Source
		}
		sb.WriteString(r.Content)
		sb.WriteByte('\n')
	}
	return strings.TrimRight(sb.String(), "\n")
}

// sourceHeader returns the human-readable section label for a source identifier.
func sourceHeader(source string) string {
	switch source {
	case string(MemoryTypeWorkspaceProfile):
		return "workspace profile"
	case string(MemoryTypeProjectFact):
		return "project facts"
	case "experiential":
		return "relevant experience"
	case "rag":
		return "knowledge"
	case "kb":
		return "project knowledge"
	default:
		return source
	}
}
