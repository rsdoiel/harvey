package harvey

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

/** MemoryType identifies the category of a memory document.
 *
 * Example:
 *   doc := &MemoryDoc{Meta: MemoryMeta{Type: MemoryTypeToolUse}}
 */
type MemoryType string

const (
	MemoryTypeToolUse        MemoryType = "tool_use"
	MemoryTypeWorkflow       MemoryType = "workflow"
	MemoryTypeUserPreference MemoryType = "user_preference"
)

// ValidMemoryTypes lists the accepted values for MemoryMeta.Type.
var ValidMemoryTypes = []MemoryType{
	MemoryTypeToolUse,
	MemoryTypeWorkflow,
	MemoryTypeUserPreference,
}

/** MemoryMeta holds the structured YAML front matter of a memory document.
 * It replaces the standard Fountain title block, so the file begins with
 * a --- delimited YAML section followed by a proper Fountain body.
 *
 * Example:
 *   meta := MemoryMeta{
 *       ID:          "git_fix_a3f891",
 *       Type:        MemoryTypeToolUse,
 *       Description: "Fixed 'fatal: not a git repository' with git init",
 *   }
 */
type MemoryMeta struct {
	ID            string         `yaml:"id"`
	Type          MemoryType     `yaml:"type"`
	CreatedAt     string         `yaml:"created_at"`
	UpdatedAt     string         `yaml:"updated_at"`
	Supersedes    []string       `yaml:"supersedes"`
	Tags          []string       `yaml:"tags"`
	Description   string         `yaml:"description"`
	Summary       string         `yaml:"summary"`
	SourceSession string         `yaml:"source_session,omitempty"`
	Metadata      map[string]any `yaml:"metadata,omitempty"`
}

/** MemoryDoc is a parsed memory file: structured YAML metadata plus a
 * Fountain-format body. The FountainBody begins at "FADE IN:" and ends
 * with "THE END." and uses the INT. MEMORY <TIMESTAMP> scene heading.
 *
 * Example:
 *   doc, err := ParseMemoryDoc(data)
 *   fmt.Println(doc.Meta.Description)
 */
type MemoryDoc struct {
	Meta         MemoryMeta
	FountainBody string // everything after the closing ---\n
}

/** ParseMemoryDoc parses a memory file from raw bytes. The file must begin
 * with a --- delimited YAML front matter block. Everything after the closing
 * --- is the Fountain body.
 *
 * Parameters:
 *   data ([]byte) — raw file contents.
 *
 * Returns:
 *   *MemoryDoc — parsed document.
 *   error      — if the front matter is missing, malformed, or the YAML
 *                cannot be decoded.
 *
 * Example:
 *   data, _ := os.ReadFile("agents/memories/tool_use/git_fix_a3f891.fountain")
 *   doc, err := ParseMemoryDoc(data)
 */
func ParseMemoryDoc(data []byte) (*MemoryDoc, error) {
	s := string(data)
	if !strings.HasPrefix(s, "---\n") {
		return nil, fmt.Errorf("memory: file does not start with YAML front matter (---)")
	}
	rest := s[4:] // skip opening ---\n
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return nil, fmt.Errorf("memory: unclosed YAML front matter: missing closing ---")
	}
	yamlPart := rest[:end]
	body := rest[end+4:] // skip \n---
	if strings.HasPrefix(body, "\n") {
		body = body[1:]
	}

	var meta MemoryMeta
	if err := yaml.Unmarshal([]byte(yamlPart), &meta); err != nil {
		return nil, fmt.Errorf("memory: parse YAML front matter: %w", err)
	}
	if meta.ID == "" {
		return nil, fmt.Errorf("memory: YAML front matter missing required field: id")
	}
	if meta.Type == "" {
		return nil, fmt.Errorf("memory: YAML front matter missing required field: type")
	}
	if meta.Supersedes == nil {
		meta.Supersedes = []string{}
	}
	if meta.Tags == nil {
		meta.Tags = []string{}
	}
	return &MemoryDoc{Meta: meta, FountainBody: body}, nil
}

/** Bytes serialises the MemoryDoc back to its file representation:
 * YAML front matter delimited by --- followed by the Fountain body.
 *
 * Returns:
 *   []byte — the file contents ready to write to disk.
 *   error  — if the YAML cannot be marshalled.
 *
 * Example:
 *   data, err := doc.Bytes()
 *   os.WriteFile(doc.FilePath("agents/memories"), data, 0o644)
 */
func (m *MemoryDoc) Bytes() ([]byte, error) {
	yamlBytes, err := yaml.Marshal(&m.Meta)
	if err != nil {
		return nil, fmt.Errorf("memory: marshal YAML front matter: %w", err)
	}
	var buf bytes.Buffer
	buf.WriteString("---\n")
	buf.Write(yamlBytes)
	buf.WriteString("---\n")
	buf.WriteString(m.FountainBody)
	return buf.Bytes(), nil
}

/** FilePath returns the canonical file path for this memory document under
 * baseDir. The path follows the pattern {baseDir}/{type}/{id}.fountain.
 *
 * Parameters:
 *   baseDir (string) — absolute path to the memories root directory
 *                      (e.g. agents/memories resolved to absolute).
 *
 * Returns:
 *   string — absolute file path for this memory.
 *
 * Example:
 *   path := doc.FilePath("/home/user/project/agents/memories")
 *   // → "/home/user/project/agents/memories/tool_use/git_fix_a3f891.fountain"
 */
func (m *MemoryDoc) FilePath(baseDir string) string {
	return filepath.Join(baseDir, string(m.Meta.Type), m.Meta.ID+".fountain")
}

/** ArchivePath returns the file path for this memory document when archived.
 * Archived files live under {baseDir}/archive/{type}/{id}.fountain.
 *
 * Parameters:
 *   baseDir (string) — absolute path to the memories root directory.
 *
 * Returns:
 *   string — absolute archive file path.
 *
 * Example:
 *   path := doc.ArchivePath("/home/user/project/agents/memories")
 *   // → "/home/user/project/agents/memories/archive/tool_use/git_fix_a3f891.fountain"
 */
func (m *MemoryDoc) ArchivePath(baseDir string) string {
	return filepath.Join(baseDir, "archive", string(m.Meta.Type), m.Meta.ID+".fountain")
}

/** EmbedText returns the text used to generate the embedding for this
 * memory. It combines description, tags, and summary to produce a compact,
 * semantically rich representation.
 *
 * Returns:
 *   string — the text to pass to the embedder.
 *
 * Example:
 *   vec, err := embedder.Embed(doc.EmbedText())
 */
func (m *MemoryDoc) EmbedText() string {
	parts := []string{m.Meta.Description}
	if len(m.Meta.Tags) > 0 {
		parts = append(parts, strings.Join(m.Meta.Tags, " "))
	}
	if m.Meta.Summary != "" {
		parts = append(parts, m.Meta.Summary)
	}
	return strings.Join(parts, " ")
}

/** NewMemoryDoc creates a new MemoryDoc with the given fields and sets
 * CreatedAt, UpdatedAt, and empty Supersedes. The caller is responsible
 * for populating FountainBody before saving.
 *
 * Parameters:
 *   id          (string)     — unique identifier (see GenerateMemoryID).
 *   memType     (MemoryType) — one of MemoryTypeToolUse, etc.
 *   description (string)     — one-sentence human summary.
 *   summary     (string)     — 2-3 sentence text optimised for embedding.
 *   tags        ([]string)   — keyword tags for filtering.
 *
 * Returns:
 *   *MemoryDoc — new document with empty FountainBody.
 *
 * Example:
 *   doc := NewMemoryDoc("git_fix_a3f891", MemoryTypeToolUse,
 *       "Fixed fatal: not a git repository", "...", []string{"git","fix"})
 */
func NewMemoryDoc(id string, memType MemoryType, description, summary string, tags []string) *MemoryDoc {
	now := time.Now().UTC().Format(time.RFC3339)
	if tags == nil {
		tags = []string{}
	}
	return &MemoryDoc{
		Meta: MemoryMeta{
			ID:          id,
			Type:        memType,
			CreatedAt:   now,
			UpdatedAt:   now,
			Supersedes:  []string{},
			Tags:        tags,
			Description: description,
			Summary:     summary,
		},
	}
}

/** GenerateMemoryID returns a new unique memory ID for the given type.
 * The format is {type}_{6-hex-suffix} where the suffix is derived from
 * the current nanosecond timestamp.
 *
 * Parameters:
 *   t (MemoryType) — the memory type used as a prefix.
 *
 * Returns:
 *   string — a new unique ID, e.g. "tool_use_a3f891".
 *
 * Example:
 *   id := GenerateMemoryID(MemoryTypeToolUse) // "tool_use_a3f891"
 */
func GenerateMemoryID(t MemoryType) string {
	suffix := fmt.Sprintf("%06x", time.Now().UnixNano()&0xffffff)
	return fmt.Sprintf("%s_%s", t, suffix)
}

/** BuildFountainBody constructs a minimal valid Fountain body for a memory
 * document. The scene heading uses the INT. MEMORY convention with the
 * provided timestamp.
 *
 * Parameters:
 *   timestamp (string)        — ISO-style timestamp, e.g. "2026-05-25 12:00:00".
 *   turns     ([][2]string)   — sequence of [character, dialogue] pairs.
 *
 * Returns:
 *   string — the complete Fountain body from FADE IN: to THE END.
 *
 * Example:
 *   body := BuildFountainBody("2026-05-25 12:00:00", [][2]string{
 *       {"RSDOIEL", "I got: fatal: not a git repository"},
 *       {"HARVEY",  "Running git init to initialize the repository."},
 *       {"RSDOIEL", "That fixed it."},
 *   })
 */
func BuildFountainBody(timestamp string, turns [][2]string) string {
	var b strings.Builder
	b.WriteString("FADE IN:\n\n")
	b.WriteString("INT. MEMORY ")
	b.WriteString(timestamp)
	b.WriteString("\n")
	for _, turn := range turns {
		b.WriteString("\n")
		b.WriteString(turn[0])
		b.WriteString("\n")
		b.WriteString(turn[1])
		b.WriteString("\n")
	}
	b.WriteString("\nTHE END.\n")
	return b.String()
}

// isValidMemoryType returns true if t is one of the recognised memory types.
func isValidMemoryType(t MemoryType) bool {
	for _, v := range ValidMemoryTypes {
		if v == t {
			return true
		}
	}
	return false
}
