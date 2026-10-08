package harvey

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// proposedMemory is the JSON shape the LLM returns for each proposed memory.
type proposedMemory struct {
	Type         string   `json:"type"`
	Kind         string   `json:"kind"`
	Description  string   `json:"description"`
	Summary      string   `json:"summary"`
	Action       string   `json:"action"`
	Tags         []string `json:"tags"`
	FountainBody string   `json:"fountain_body"`
}

const minerSystemPrompt = `You are a memory extraction assistant for Harvey, a terminal coding agent.

OUTPUT FORMAT — STRICT: Your entire response must be a valid JSON array.
- Start your response with [ and end with ]
- No prose, no explanation, no markdown fences, no Fountain screenplay text
- If nothing is worth saving, respond with exactly: []

Your task: read the session transcript and propose 0-5 memories worth saving.
Each memory captures one reusable insight: a tool_use trick, a workflow, a user_preference, a workspace_profile fact, or a project_fact.

Each object in the JSON array must have exactly these fields:
  type          string  — one of: "tool_use", "workflow", "user_preference", "workspace_profile", "project_fact"
  kind          string  — one of: "pitfall", "workaround", "recommendation", "pattern", or ""
  description   string  — one sentence, action-oriented (e.g. "Run git init when git reports 'not a repository'")
  summary       string  — 2-3 sentences explaining what happened and why it matters
  action        string  — imperative sentence: the concrete step a future agent should take (use "" when none applies)
  tags          array   — 3-7 lowercase keyword strings
  fountain_body string  — a short Fountain scene as a plain string value inside the JSON (not screenplay format outside the JSON)

Kind values:
  pitfall        — a permanent gotcha (API quirk, undocumented behaviour, subtle invariant)
  workaround     — useful now but may become obsolete with better tooling
  recommendation — points to the right approach, tool, or pattern; "prefer X over Y for Z"
  pattern        — a recurring successful approach worth repeating
  ""             — leave empty when none of the above apply

Rules:
- Only include things a future session would actually benefit from knowing.
- Avoid one-off debugging details; focus on durable, transferable knowledge.

Example of the required output format (one memory):
[{"type":"tool_use","kind":"pitfall","description":"Always quote file paths containing spaces in shell commands.","summary":"Unquoted paths with spaces cause argument splitting. This is a permanent shell behaviour.","action":"Wrap any path that may contain spaces in double quotes.","tags":["shell","paths","quoting"],"fountain_body":"FADE IN:\n\nINT. MEMORY 2026-01-01 00:00:00\n\nHARVEY\nQuote your paths.\n\nTHE END.\n"}]`

// extractJSON pulls a JSON array out of possibly-fenced LLM output.
func extractJSON(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "```") {
		start := strings.Index(raw, "\n")
		end := strings.LastIndex(raw, "```")
		if start > 0 && end > start {
			raw = strings.TrimSpace(raw[start+1 : end])
		}
	}
	first := strings.Index(raw, "[")
	last := strings.LastIndex(raw, "]")
	if first < 0 || last <= first {
		return "", false
	}
	return raw[first : last+1], true
}

/** errBadReply marks a model reply the miner could not turn into a list of
 * memories: no JSON array after one retry, or an array that does not parse.
 * With ErrStreamTruncated it means "this model could not process this
 * session", as opposed to a server that was unreachable.
 *
 * Example:
 *   if errors.Is(err, errBadReply) {
 *       fmt.Println("the model could not do it")
 *   }
 */
var errBadReply = errors.New("reply was not a JSON list of memories")

/** sessionIsEmpty reports whether a recorded session has no dialogue to mine:
 * after the title page, only blank lines, FADE IN/OUT, THE END, scene
 * headings and model-switch notes.
 *
 * Parameters:
 *   text (string) — the full session file.
 *
 * Returns:
 *   bool — true when there is nothing for a model to read.
 *
 * Example:
 *   if sessionIsEmpty(string(data)) { fmt.Println("empty") }
 */
func sessionIsEmpty(text string) bool {
	inTitle := true
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if inTitle {
			// The title page is the leading run of "Key: value" lines.
			if line == "" {
				inTitle = false
				continue
			}
			if k, _, ok := strings.Cut(line, ":"); ok && !strings.ContainsAny(k, ".[") {
				continue
			}
			inTitle = false
		}
		switch {
		case line == "":
		case line == "FADE IN:", line == "FADE OUT.", line == "THE END.":
		case strings.HasPrefix(line, "INT. "), strings.HasPrefix(line, "EXT. "):
		case strings.HasPrefix(line, "[[model switch:"):
		default:
			return false
		}
	}
	return true
}

/** minerModel returns the engine-qualified key of the model that will answer
 * the agent's mining request, for remembering which model failed on a session.
 *
 * Parameters:
 *   agent (*Agent) — the agent whose Client does the extraction.
 *
 * Returns:
 *   string — "engine/model" (bare model for Ollama), or the client's Name().
 *
 * Example:
 *   key := minerModel(agent) // "hailo/qwen2.5-coder:1.5b"
 */
func minerModel(agent *Agent) string {
	if mn, ok := agent.Client.(interface{ ModelName() string }); ok && mn.ModelName() != "" {
		return agent.modelKey(mn.ModelName())
	}
	return agent.Client.Name()
}

// unprocessed turns an extraction error into the report for a session the
// model could not process. It records the failure and leaves the session
// unmined. Errors that say nothing about the model (an unreachable server, a
// cancelled context) are returned unchanged. The second result is false in
// that case.
func (m *Miner) unprocessed(sessionPath string, agent *Agent, err error) (error, bool) {
	tooSmall := errors.Is(err, errPromptLimitTooSmall)
	if !isPromptTooLargeFailure(err) && !errors.Is(err, errBadReply) && !tooSmall {
		return err, false
	}
	model := minerModel(agent)
	reason := "the reply was cut off before it finished, even on the smallest parts it was given"
	switch {
	case errors.Is(err, errBadReply):
		reason = "the reply was not a JSON list of memories"
	case tooSmall:
		reason = err.Error()
	}
	m.manifest.RecordFailure(sessionPath, model, reason)
	if saveErr := m.manifest.Save(m.store.Dir()); saveErr != nil {
		reason += fmt.Sprintf(" (could not save the manifest: %v)", saveErr)
	}
	return Negativef("%s could not be processed by %s: %s. It is left unmined; "+
		"use /model use to pick another model, then /memory mine", filepath.Base(sessionPath), model, reason), true
}

// The smallest slice of dialogue worth asking a model about, in estimateTokens
// units, and the room kept free beyond the instructions for the framing the
// server adds.
const (
	minerMinChunkTokens = 150
	minerPromptMargin   = 50
)

// minerContinuation opens every part of a session after the first.
const minerContinuation = "(This is a later part of one session transcript; the earlier parts were read separately.)\n\n"

/** errPromptLimitTooSmall marks a model whose prompt limit cannot hold the
 * mining instructions plus a useful amount of dialogue. Like errBadReply it
 * means this model cannot mine, whatever the session.
 *
 * Example:
 *   if errors.Is(err, errPromptLimitTooSmall) { fmt.Println("pick a model with room") }
 */
var errPromptLimitTooSmall = errors.New("prompt limit too small to mine with")

/** minerChunkTokens returns how many tokens of session text one mining request
 * may carry for a model whose whole-prompt limit is limit: the limit less the
 * mining instructions and a margin.
 *
 * Parameters:
 *   limit (int) — the model's prompt limit in estimateTokens units; 0 when unknown.
 *
 * Returns:
 *   int   — tokens of session text per request; 0 means no cap (limit unknown).
 *   error — wrapping errPromptLimitTooSmall when less than a useful slice is left.
 *
 * Example:
 *   n, err := minerChunkTokens(agent.promptTokenLimit())
 */
func minerChunkTokens(limit int) (int, error) {
	if limit <= 0 {
		return 0, nil
	}
	n := limit - estimateTokens(minerSystemPrompt) - minerPromptMargin
	if n < minerMinChunkTokens {
		return 0, fmt.Errorf("%w: a limit of %d tokens leaves %d for dialogue after the instructions", errPromptLimitTooSmall, limit, n)
	}
	return n, nil
}

/** takeChunk cuts the front off text for one mining request of at most
 * sizeTokens (4 bytes each). It packs whole paragraphs (blocks separated by a
 * blank line, which in a recorded session are the turns and their
 * continuations); a paragraph larger than the budget is cut at a line
 * boundary, and a single line larger than it at a character boundary. The
 * chunks always add up to the text, and the chunk is never empty.
 *
 * Parameters:
 *   text       (string) — the session text still to read.
 *   sizeTokens (int)    — budget in estimateTokens units; 0 or less means no cap.
 *
 * Returns:
 *   chunk (string) — the front of text to send.
 *   rest  (string) — the remainder, "" when chunk is all of it.
 *
 * Example:
 *   chunk, rest := takeChunk(sessionText, 500)
 */
func takeChunk(text string, sizeTokens int) (chunk, rest string) {
	maxBytes := sizeTokens * 4
	if sizeTokens <= 0 || len(text) <= maxBytes {
		return text, ""
	}
	cut := 0
	for pos := 0; pos < len(text); {
		end := len(text)
		if i := strings.Index(text[pos:], "\n\n"); i >= 0 {
			end = pos + i + 2
			for end < len(text) && text[end] == '\n' {
				end++
			}
		}
		if end > maxBytes {
			break
		}
		cut, pos = end, end
	}
	if cut == 0 {
		cut = strings.LastIndexByte(text[:maxBytes], '\n') + 1
	}
	if cut == 0 {
		cut = maxBytes
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		if cut == 0 {
			cut = maxBytes
		}
	}
	return text[:cut], text[cut:]
}

// dedupeDocs drops proposals whose description repeats an earlier one's, so a
// lesson that came up in several parts of a session is proposed once.
func dedupeDocs(docs []MemoryDoc) []MemoryDoc {
	seen := make(map[string]bool, len(docs))
	var out []MemoryDoc
	for _, d := range docs {
		key := strings.ToLower(strings.Join(strings.Fields(d.Meta.Description), " "))
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, d)
	}
	return out
}

// extractAll proposes memories from the whole session. A session that fits the
// model goes in one request. When the model's prompt limit is known the session
// is read in parts that fit it; when it is not, the whole session is tried and
// a request the model cannot take (truncated, too large, or answered with
// something other than a JSON list, which small models do on long input) halves the size,
// which then holds for the rest of the session. Proposals from all parts are
// merged. Any part the model cannot process fails the whole session, so a
// half-read session is never mined.
func (m *Miner) extractAll(ctx context.Context, text string, agent *Agent, out io.Writer) ([]MemoryDoc, error) {
	size, err := minerChunkTokens(agent.promptTokenLimit())
	if err != nil {
		return nil, err
	}
	var docs []MemoryDoc
	announced := false
	for part, rest := 0, text; rest != ""; {
		chunk, next := takeChunk(rest, size)
		if next != "" && !announced {
			fmt.Fprintln(out, dim("  Session is large for this model; reading it in parts."))
			announced = true
		}
		in := chunk
		if part > 0 {
			in = minerContinuation + chunk
		}
		got, err := m.extract(ctx, in, agent, out)
		if err != nil {
			if isPromptTooLargeFailure(err) || errors.Is(err, errBadReply) {
				if half := estimateTokens(chunk) / 2; half >= minerMinChunkTokens && (size == 0 || half < size) {
					size = half
					continue
				}
			}
			return nil, err
		}
		docs = append(docs, got...)
		rest = next
		part++
	}
	return dedupeDocs(docs), nil
}

/** Miner drives the mining and interactive review pipeline for a Harvey
 * session file. It calls the LLM once to propose memories, then runs an
 * interactive REPL for the user to accept, edit, skip, or supersede each
 * proposed memory. The session is recorded in the manifest only after the
 * full review completes.
 *
 * Example:
 *   miner := NewMiner(store, manifest, ws)
 *   err := miner.Mine(ctx, "agents/sessions/foo.spmd", agent, embedder, os.Stdout, os.Stdin)
 */
type Miner struct {
	store    *MemoryStore
	manifest *Manifest
	ws       *Workspace
}

/** NewMiner creates a Miner that writes to store, tracks sessions in
 * manifest, and normalises workspace paths using ws.
 *
 * Parameters:
 *   store    (*MemoryStore) — destination memory store.
 *   manifest (*Manifest)   — manifest tracking mined sessions.
 *   ws       (*Workspace)  — workspace for path normalisation.
 *
 * Returns:
 *   *Miner — ready miner.
 *
 * Example:
 *   miner := NewMiner(store, manifest, ws)
 */
func NewMiner(store *MemoryStore, manifest *Manifest, ws *Workspace) *Miner {
	return &Miner{store: store, manifest: manifest, ws: ws}
}

/** Mine reads sessionPath, asks the LLM to propose memories, runs the
 * interactive review loop, and records the session in the manifest when
 * the review completes fully. An interrupted review (quit) leaves the
 * session absent from the manifest so it is re-offered on the next run.
 *
 * Parameters:
 *   ctx         (context.Context) — cancellation context.
 *   sessionPath (string)          — absolute path to the .spmd session file.
 *   agent       (*Agent)          — Harvey agent providing the LLM client.
 *   embedder    (Embedder)        — used for near-duplicate detection and
 *                                   embedding; nil disables both.
 *   out         (io.Writer)       — where prompts and results are written.
 *   in          (io.Reader)       — where user input is read.
 *
 * Returns:
 *   error — on session read, LLM, or store failure.
 *
 * Example:
 *   err := miner.Mine(ctx, "agents/sessions/foo.spmd", agent, embedder, os.Stdout, os.Stdin)
 */
func (m *Miner) Mine(ctx context.Context, sessionPath string, agent *Agent, embedder Embedder, out io.Writer, in io.Reader) error {
	if m.manifest.IsMined(sessionPath) {
		fmt.Fprintf(out, "Session %s is already mined. Use --force to re-mine.\n", sessionPath)
		return nil
	}

	sessionData, err := os.ReadFile(sessionPath)
	if err != nil {
		return fmt.Errorf("mine: read session: %w", err)
	}

	if sessionIsEmpty(string(sessionData)) {
		fmt.Fprintf(out, "Session %s is empty (no dialogue); nothing to mine. Recorded as empty.\n", sessionPath)
		m.manifest.RecordEmpty(sessionPath)
		return m.manifest.Save(m.store.Dir())
	}

	fmt.Fprintf(out, "Extracting memories from %s …\n", sessionPath)
	proposed, err := m.extractAll(ctx, string(sessionData), agent, out)
	if err != nil {
		if report, handled := m.unprocessed(sessionPath, agent, err); handled {
			return report
		}
		return fmt.Errorf("mine: extract: %w", err)
	}

	if len(proposed) == 0 {
		fmt.Fprintln(out, "No memories proposed for this session.")
		m.manifest.Record(sessionPath, nil, 0)
		return m.manifest.Save(m.store.Dir())
	}

	fmt.Fprintf(out, "LLM proposed %d memory candidate(s). Starting review…\n\n", len(proposed))

	accepted, skipped, quit, err := m.reviewInteractive(proposed, embedder, m.ws.Root, out, in)
	if quit {
		fmt.Fprintln(out, "\nReview quit early — session will be re-offered on the next /memory mine run.")
		return err
	}
	if err != nil {
		return err
	}

	m.manifest.Record(sessionPath, accepted, skipped)
	if saveErr := m.manifest.Save(m.store.Dir()); saveErr != nil {
		fmt.Fprintf(out, "Warning: could not save manifest: %v\n", saveErr)
	}
	fmt.Fprintf(out, "\nDone. Accepted: %d  Skipped: %d\n", len(accepted), skipped)
	return nil
}

// extract calls the LLM once to propose memories from sessionText, retrying
// once if the response is not valid JSON.
func (m *Miner) extract(ctx context.Context, sessionText string, agent *Agent, out io.Writer) ([]MemoryDoc, error) {
	if agent.Client == nil {
		return nil, fmt.Errorf("extract: no LLM client available")
	}

	msgs := []Message{
		{Role: "system", Content: minerSystemPrompt},
		{Role: "user", Content: sessionText},
	}

	var buf bytes.Buffer
	if _, err := agent.Client.Chat(ctx, msgs, &buf); err != nil {
		return nil, fmt.Errorf("extract: LLM call: %w", err)
	}
	raw := buf.String()

	jsonStr, ok := extractJSON(raw)
	if !ok {
		msgs = append(msgs,
			Message{Role: "assistant", Content: raw},
			Message{Role: "user", Content: "Please output only a valid JSON array as instructed."},
		)
		var buf2 bytes.Buffer
		if _, err := agent.Client.Chat(ctx, msgs, &buf2); err != nil {
			return nil, fmt.Errorf("extract: retry LLM call: %w", err)
		}
		raw = buf2.String()
		jsonStr, ok = extractJSON(raw)
		if !ok {
			return nil, fmt.Errorf("extract: could not find JSON array in LLM response: %w", errBadReply)
		}
	}

	var proposals []proposedMemory
	if err := json.Unmarshal([]byte(jsonStr), &proposals); err != nil {
		return nil, fmt.Errorf("extract: parse JSON: %w: %w\nraw: %s", errBadReply, err, jsonStr)
	}

	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	var docs []MemoryDoc
	for _, p := range proposals {
		mt := MemoryType(p.Type)
		if !isValidMemoryType(mt) {
			mt = MemoryTypeToolUse
		}
		id := GenerateMemoryID(mt)
		doc := NewMemoryDoc(id, mt, p.Description, p.Summary, p.Tags)
		doc.Meta.Kind = p.Kind
		doc.Meta.Action = p.Action
		doc.Meta.Confidence = 0.5
		if p.FountainBody != "" {
			doc.FountainBody = p.FountainBody
		} else {
			doc.FountainBody = BuildFountainBody(now, nil)
		}
		docs = append(docs, *doc)
	}
	return docs, nil
}

// reviewInteractive presents each proposed memory to the user in a REPL.
// Returns accepted IDs, skipped count, whether the user quit early, and any error.
func (m *Miner) reviewInteractive(proposed []MemoryDoc, embedder Embedder, workspacePath string, out io.Writer, in io.Reader) (accepted []string, skipped int, quit bool, err error) {
	reader := newLineReader(in)
	total := len(proposed)

	for i := range proposed {
		doc := &proposed[i]

		scrubbed, scrubResult, scrubErr := ScrubDoc(doc, workspacePath)
		if scrubErr == nil {
			doc = scrubbed
		}

		var dupes []MemoryMeta
		if embedder != nil {
			dupes, _ = m.nearDuplicates(doc, embedder)
		}

		fmt.Fprintf(out, "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
		fmt.Fprintf(out, " Proposed memory %d of %d\n", i+1, total)
		fmt.Fprintf(out, "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
		fmt.Fprintf(out, " Type:        %s\n", doc.Meta.Type)
		if doc.Meta.Kind != "" {
			fmt.Fprintf(out, " Kind:        %s\n", doc.Meta.Kind)
		}
		fmt.Fprintf(out, " Description: %s\n", doc.Meta.Description)
		if doc.Meta.Action != "" {
			fmt.Fprintf(out, " Action:      %s\n", doc.Meta.Action)
		}
		fmt.Fprintf(out, " Tags:        %s\n", strings.Join(doc.Meta.Tags, ", "))
		if doc.Meta.Summary != "" {
			fmt.Fprintf(out, " Summary:     %s\n", doc.Meta.Summary)
		}
		if len(scrubResult.Flags) > 0 {
			fmt.Fprintln(out)
			for _, f := range scrubResult.Flags {
				fmt.Fprintf(out, " ⚠  Sensitive pattern: %s\n", f)
			}
		}
		if len(dupes) > 0 {
			fmt.Fprintln(out)
			for _, d := range dupes {
				fmt.Fprintf(out, " ~ Similar existing memory: %s — %s\n", d.ID, d.Description)
			}
		}
		fmt.Fprintln(out)

		done := false
		for !done {
			fmt.Fprintf(out, "[a]ccept  [e]dit  [s]how similar  [r]eplace <id>  [f]ull view  [k]skip  [q]uit\n> ")
			line, readErr := reader.ReadString('\n')
			line = strings.TrimSpace(line)
			if readErr != nil && line == "" {
				// Input ended (Ctrl-D, a closed pipe): treat it as quit, or the
				// prompt would ask again for ever.
				return accepted, skipped, true, nil
			}

			switch {
			case line == "a" || line == "accept":
				if saveErr := m.store.Save(doc, embedder); saveErr != nil {
					fmt.Fprintf(out, "Error saving memory: %v\n", saveErr)
				} else {
					accepted = append(accepted, doc.Meta.ID)
					fmt.Fprintf(out, "✓ Saved as %s\n\n", doc.Meta.ID)
				}
				done = true

			case line == "k" || line == "skip":
				skipped++
				fmt.Fprintln(out, "Skipped.")
				fmt.Fprintln(out)
				done = true

			case line == "q" || line == "quit":
				return accepted, skipped, true, nil

			case line == "f" || line == "full":
				data, _ := doc.Bytes()
				fmt.Fprintf(out, "\n%s\n", string(data))

			case line == "s" || line == "show":
				if len(dupes) == 0 {
					fmt.Fprintln(out, "No similar memories found.")
				} else {
					for _, d := range dupes {
						existing, err2 := m.store.ByID(d.ID)
						if err2 != nil || existing == nil {
							fmt.Fprintf(out, "  %s: (not found)\n", d.ID)
							continue
						}
						fmt.Fprintf(out, "\n--- %s ---\n%s---\n", d.ID, existing.FountainBody)
					}
				}

			case strings.HasPrefix(line, "r ") || strings.HasPrefix(line, "replace "):
				parts := strings.Fields(line)
				if len(parts) < 2 {
					fmt.Fprintln(out, "Usage: r <id>")
					continue
				}
				replaceID := parts[1]
				doc.Meta.Supersedes = append(doc.Meta.Supersedes, replaceID)
				if archErr := m.store.Archive(replaceID); archErr != nil {
					fmt.Fprintf(out, "Warning: could not archive %s: %v\n", replaceID, archErr)
				}
				if saveErr := m.store.Save(doc, embedder); saveErr != nil {
					fmt.Fprintf(out, "Error saving memory: %v\n", saveErr)
				} else {
					accepted = append(accepted, doc.Meta.ID)
					fmt.Fprintf(out, "✓ Saved as %s (supersedes %s)\n\n", doc.Meta.ID, replaceID)
				}
				done = true

			case line == "e" || line == "edit":
				edited, editErr := editInEditor(doc, out)
				if editErr != nil {
					fmt.Fprintf(out, "Edit failed: %v\n", editErr)
				} else {
					doc = edited
					fmt.Fprintln(out, "Memory updated (not yet saved — choose [a]ccept to save).")
				}

			default:
				fmt.Fprintln(out, "Unknown command. Options: a, e, s, r <id>, f, k, q")
			}
		}
	}
	return accepted, skipped, false, nil
}

// nearDuplicates returns metadata for non-archived memories with cosine
// similarity >= 0.80 to doc's embedding.
func (m *Miner) nearDuplicates(doc *MemoryDoc, embedder Embedder) ([]MemoryMeta, error) {
	vec, err := embedder.Embed(doc.EmbedText())
	if err != nil {
		return nil, err
	}
	rows, err := m.store.db.Query(
		`SELECT id, type, description, summary, tags, source_session, created_at, updated_at, embedding
		 FROM memories WHERE archived=0`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []MemoryMeta
	for rows.Next() {
		var mm MemoryMeta
		var tagsJSON string
		var blob []byte
		if err := rows.Scan(
			&mm.ID, (*string)(&mm.Type), &mm.Description, &mm.Summary,
			&tagsJSON, &mm.SourceSession, &mm.CreatedAt, &mm.UpdatedAt, &blob,
		); err != nil {
			continue
		}
		v, err := deserialize(blob)
		if err != nil {
			continue
		}
		if cosineSimilarity(vec, v) >= 0.80 {
			_ = json.Unmarshal([]byte(tagsJSON), &mm.Tags)
			out = append(out, mm)
		}
	}
	return out, rows.Err()
}

/** MineAuto runs non-interactive memory extraction on sessionPath. All
 * proposed memories that are not near-duplicates of existing memories are
 * saved without user review. The session is recorded in the manifest so that
 * a subsequent /memory mine run skips it (or reports "already mined").
 *
 * Parameters:
 *   ctx         (context.Context) — cancellation context.
 *   sessionPath (string)          — absolute path to the session file.
 *   agent       (*Agent)          — provides the LLM client for extraction.
 *   embedder    (Embedder)        — used for near-duplicate detection; may be nil.
 *   out         (io.Writer)       — status output.
 *
 * Returns:
 *   error — on session read, LLM, or store failure.
 *
 * Example:
 *   miner.MineAuto(ctx, "/agents/sessions/foo.spmd", agent, embedder, os.Stdout)
 */
func (m *Miner) MineAuto(ctx context.Context, sessionPath string, agent *Agent, embedder Embedder, out io.Writer) error {
	if m.manifest.IsMined(sessionPath) {
		return nil
	}
	if agent.Client == nil {
		return nil
	}

	sessionData, err := os.ReadFile(sessionPath)
	if err != nil {
		return fmt.Errorf("auto-mine: read session: %w", err)
	}

	if sessionIsEmpty(string(sessionData)) {
		fmt.Fprintln(out, dim("  [session is empty; nothing to mine]"))
		m.manifest.RecordEmpty(sessionPath)
		return m.manifest.Save(m.store.Dir())
	}
	if m.manifest.FailedOn(sessionPath, minerModel(agent)) {
		return nil // this model already failed on it; /memory mine retries on request
	}

	proposed, err := m.extractAll(ctx, string(sessionData), agent, out)
	if err != nil {
		if report, handled := m.unprocessed(sessionPath, agent, err); handled {
			return report
		}
		return fmt.Errorf("auto-mine: extract: %w", err)
	}

	var accepted []string
	skipped := 0
	for i := range proposed {
		doc := &proposed[i]

		if scrubbed, _, scrubErr := ScrubDoc(doc, m.ws.Root); scrubErr == nil {
			doc = scrubbed
		}

		if embedder != nil {
			if dupes, _ := m.nearDuplicates(doc, embedder); len(dupes) > 0 {
				skipped++
				continue
			}
		}

		if saveErr := m.store.Save(doc, embedder); saveErr != nil {
			fmt.Fprintf(out, "%s auto-mine: save %s: %v\n", yellow("  ✗"), doc.Meta.ID, saveErr)
		} else {
			accepted = append(accepted, doc.Meta.ID)
		}
	}

	m.manifest.Record(sessionPath, accepted, skipped)
	if saveErr := m.manifest.Save(m.store.Dir()); saveErr != nil {
		return fmt.Errorf("auto-mine: save manifest: %w", saveErr)
	}

	if len(accepted) > 0 {
		_ = m.store.WriteDigest(filepath.Join(m.store.Dir(), "DIGEST.md"))
		fmt.Fprintln(out, dim(fmt.Sprintf("  [auto-mined %d %s from session]",
			len(accepted), pluralise("memory", "memories", len(accepted)))))
	}
	return nil
}

// pluralise returns singular when n == 1, otherwise plural.
func pluralise(singular, plural string, n int) string {
	if n == 1 {
		return singular
	}
	return plural
}

// editInEditor opens doc in the user's editor and re-parses the result on close.
// The round trip is editTextInEditor; this adds only the MemoryDoc serialization.
func editInEditor(doc *MemoryDoc, out io.Writer) (*MemoryDoc, error) {
	data, err := doc.Bytes()
	if err != nil {
		return nil, err
	}
	edited, err := editTextInEditor(string(data), ".fountain")
	if err != nil {
		return nil, err
	}
	return ParseMemoryDoc([]byte(edited))
}

// modelSegment holds a contiguous block of session text attributed to one model.
type modelSegment struct {
	model   string
	backend string
	text    string
}

/** parseModelSwitchNote parses a Fountain model-switch note of the form:
 *   [[model switch: NAME (BACKEND) at TIMESTAMP]]
 * Returns (name, backend, true) on success; ("", "", false) otherwise.
 *
 * Parameters:
 *   line (string) — a single line from a session file.
 *
 * Returns:
 *   name    (string) — model name.
 *   backend (string) — backend identifier.
 *   ok      (bool)   — true when the line matches the expected format.
 *
 * Example:
 *   name, backend, ok := parseModelSwitchNote("[[model switch: phi-mini (llamafile) at 2026-06-20 14:32:11]]")
 */
func parseModelSwitchNote(line string) (name, backend string, ok bool) {
	const prefix = "[[model switch: "
	const suffix = "]]"
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, prefix) || !strings.HasSuffix(line, suffix) {
		return "", "", false
	}
	inner := line[len(prefix) : len(line)-len(suffix)] // "NAME (BACKEND) at TIMESTAMP"
	// Find " (" to split name from the rest.
	openParen := strings.Index(inner, " (")
	if openParen < 0 {
		return "", "", false
	}
	name = inner[:openParen]
	rest := inner[openParen+2:] // "BACKEND) at TIMESTAMP"
	closeParen := strings.Index(rest, ")")
	if closeParen < 0 {
		return "", "", false
	}
	backend = rest[:closeParen]
	return name, backend, true
}

/** splitAtModelSwitches partitions session text into segments, each attributed
 * to the model that was active when those turns were generated. The first
 * segment uses startModel/startBackend; subsequent segments use the name and
 * backend parsed from each [[model switch: ...]] note.
 *
 * Parameters:
 *   text         (string) — full session file content.
 *   startModel   (string) — model active at session start.
 *   startBackend (string) — backend active at session start.
 *
 * Returns:
 *   []modelSegment — one entry per model-attributed block.
 *
 * Example:
 *   segs := splitAtModelSwitches(spmd, "qwen-coding", "llamafile")
 */
func splitAtModelSwitches(text, startModel, startBackend string) []modelSegment {
	var segs []modelSegment
	currentModel := startModel
	currentBackend := startBackend
	var buf strings.Builder

	for _, line := range strings.Split(text, "\n") {
		if name, backend, ok := parseModelSwitchNote(line); ok {
			segs = append(segs, modelSegment{
				model:   currentModel,
				backend: currentBackend,
				text:    buf.String(),
			})
			buf.Reset()
			currentModel = name
			currentBackend = backend
			continue
		}
		buf.WriteString(line)
		buf.WriteByte('\n')
	}
	// Final segment.
	segs = append(segs, modelSegment{
		model:   currentModel,
		backend: currentBackend,
		text:    buf.String(),
	})
	return segs
}
