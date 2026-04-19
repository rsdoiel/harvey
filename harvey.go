package harvey

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

/** ChatStats holds timing and token-count data returned by a backend after
 * each Chat call. Fields are zero for backends that do not report them.
 *
 * Fields:
 *   PromptTokens (int)           — tokens in the prompt/history sent.
 *   ReplyTokens  (int)           — tokens in the generated response.
 *   Elapsed      (time.Duration) — wall-clock time for the full call.
 *   TokensPerSec (float64)       — generation throughput (reply tokens / s).
 *
 * Example:
 *   stats, err := client.Chat(ctx, history, &buf)
 *   fmt.Println(stats.Format())
 */
type ChatStats struct {
	PromptTokens int
	ReplyTokens  int
	Elapsed      time.Duration
	TokensPerSec float64
}

/** Format returns a human-readable one-line summary of the stats, e.g.
 * "26 prompt + 42 reply tokens · 8.3s · 5.1 tok/s".
 * If token counts are zero (backend does not report them) only elapsed time
 * is returned.
 *
 * Returns:
 *   string — formatted stats line.
 *
 * Example:
 *   fmt.Println(stats.Format()) // "26 prompt + 42 reply tokens · 8.3s · 5.1 tok/s"
 */
func (s ChatStats) Format() string {
	elapsed := s.Elapsed.Round(time.Millisecond)
	if s.ReplyTokens == 0 {
		return fmt.Sprintf("%s", elapsed)
	}
	return fmt.Sprintf("%d prompt + %d reply tokens · %s · %.1f tok/s",
		s.PromptTokens, s.ReplyTokens,
		elapsed, s.TokensPerSec)
}

/** Message represents a single chat message exchanged with a backend.
 *
 * Fields:
 *   Role    (string) — "system", "user", or "assistant".
 *   Content (string) — the message body.
 *
 * Example:
 *   msg := Message{Role: "user", Content: "Hello!"}
 */
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

/** LLMClient is the interface implemented by each LLM backend (Ollama,
 * publicai.co, etc.).
 *
 * Methods:
 *   Name()   string                                    — human-readable backend identifier.
 *   Chat()   func(ctx, messages, out) (ChatStats, error) — send history, stream reply to out.
 *   Models() func(ctx) ([]string, error)                — list available models.
 *   Close()  error                                      — release held resources.
 *
 * Example:
 *   var c LLMClient = NewOllamaClient("http://localhost:11434", "llama3")
 *   stats, err := c.Chat(ctx, history, os.Stdout)
 */
type LLMClient interface {
	// Name returns a human-readable identifier shown in the UI.
	Name() string
	// Chat sends the full conversation history, streams the response to out,
	// and returns timing and token-count stats for the call.
	Chat(ctx context.Context, messages []Message, out io.Writer) (ChatStats, error)
	// Models lists models available on this backend.
	Models(ctx context.Context) ([]string, error)
	// Close releases any resources held by the client.
	Close() error
}

// maxStatHistory is the number of past turns retained for duration estimation.
const maxStatHistory = 5

/** Agent holds the state of an active Harvey session, including the LLM
 * backend, conversation history, workspace, knowledge base, and registered
 * slash commands.
 *
 * Fields:
 *   SM        (*SessionManager) — persists conversation turns to SQLite; nil if unavailable.
 *   SessionID (int64)           — row ID of the active session in sessions.db; 0 if none.
 *   Skills    (SkillCatalog)    — skills discovered at startup; nil until loadSkills runs.
 *
 * Example:
 *   cfg := DefaultConfig()
 *   ws, _ := NewWorkspace(".")
 *   agent := NewAgent(cfg, ws)
 */
type Agent struct {
	Client        LLMClient
	Config        *Config
	History       []Message
	Workspace     *Workspace
	KB            *KnowledgeBase
	SM            *SessionManager // session persistence; nil if unavailable
	SessionID     int64           // active session row ID; 0 = no session
	Skills        SkillCatalog    // skills discovered at startup; nil until loadSkills runs
	Recorder      *Recorder
	In            io.Reader // source for interactive prompts; defaults to os.Stdin
	PinnedContext string    // persists across /clear; re-injected after system prompt
	AgentMode     bool      // when true, auto-apply tagged blocks and auto-run extracted commands
	Router        *Router   // multi-model router; nil when routing is disabled
	commands      map[string]*Command
	statHistory   []ChatStats // rolling window of recent turn stats
}

/** NewAgent creates an Agent from cfg and ws with an empty conversation
 * history. The knowledge base is opened lazily — it is nil if
 * OpenKnowledgeBase has not been called.
 *
 * Parameters:
 *   cfg (*Config)    — runtime configuration.
 *   ws  (*Workspace) — workspace that anchors all file I/O.
 *
 * Returns:
 *   *Agent — initialised agent ready for Run().
 *
 * Example:
 *   ws, _ := NewWorkspace(".")
 *   agent := NewAgent(DefaultConfig(), ws)
 */
func NewAgent(cfg *Config, ws *Workspace) *Agent {
	return &Agent{
		Config:    cfg,
		Workspace: ws,
		In:        os.Stdin,
		commands:  make(map[string]*Command),
	}
}

/** AddMessage appends a message to the conversation history.
 *
 * Parameters:
 *   role    (string) — "system", "user", or "assistant".
 *   content (string) — message body.
 *
 * Example:
 *   agent.AddMessage("user", "What is the capital of France?")
 */
func (a *Agent) AddMessage(role, content string) {
	a.History = append(a.History, Message{Role: role, Content: content})
}

/** ClearHistory resets the conversation, re-injecting the system prompt if
 * one is configured.
 *
 * Example:
 *   agent.ClearHistory()
 */
func (a *Agent) ClearHistory() {
	if a.Config.SystemPrompt != "" {
		a.History = []Message{{Role: "system", Content: a.Config.SystemPrompt}}
	} else {
		a.History = nil
	}
	if a.PinnedContext != "" {
		a.AddMessage("user", "[pinned context]\n\n"+a.PinnedContext)
	}
}

/** recordStats appends s to the rolling stat history, discarding the oldest
 * entry once maxStatHistory is exceeded. Only turns with token data (i.e.
 * from Ollama) are meaningful for estimation, but all turns are stored so
 * the window reflects real elapsed time even for backends without token counts.
 *
 * Parameters:
 *   s (ChatStats) — stats from the most recently completed turn.
 *
 * Example:
 *   agent.recordStats(stats)
 */
func (a *Agent) recordStats(s ChatStats) {
	a.statHistory = append(a.statHistory, s)
	if len(a.statHistory) > maxStatHistory {
		a.statHistory = a.statHistory[len(a.statHistory)-maxStatHistory:]
	}
}

/** estimateDuration returns a rough estimate of how long the next turn will
 * take, based on the average reply-token count and generation speed seen in
 * recent turns. Returns 0 if there is insufficient history or no turn with
 * token data has been recorded yet.
 *
 * Returns:
 *   time.Duration — estimated processing time, rounded to the nearest second.
 *                   0 means "no estimate available".
 *
 * Example:
 *   est := agent.estimateDuration()
 *   sp := newSpinner(os.Stdout, est)
 */
func (a *Agent) estimateDuration() time.Duration {
	var totalTokens, totalSec float64
	var n int
	for _, s := range a.statHistory {
		if s.ReplyTokens > 0 && s.TokensPerSec > 0 {
			totalTokens += float64(s.ReplyTokens)
			totalSec += float64(s.ReplyTokens) / s.TokensPerSec
			n++
		}
	}
	if n == 0 {
		return 0
	}
	_ = totalTokens // kept for future prompt-ratio work
	avgSec := totalSec / float64(n)
	return time.Duration(avgSec * float64(time.Second)).Round(time.Second)
}

/** ExpandDynamicSections replaces marker comments in content with live
 * workspace data. Supported markers:
 *
 *   <!-- @date -->        current date (YYYY-MM-DD)
 *   <!-- @files -->       workspace file tree, skipping hidden directories
 *   <!-- @git-status -->  output of "git status --short" in the workspace root
 *
 * If ws is nil the content is returned unchanged.
 *
 * Parameters:
 *   content (string)     — text to expand (typically HARVEY.md contents).
 *   ws      (*Workspace) — workspace used to resolve files and run git.
 *
 * Returns:
 *   string — content with all recognised markers replaced.
 *
 * Example:
 *   expanded := ExpandDynamicSections(raw, ws)
 *   agent.AddMessage("system", expanded)
 */
func ExpandDynamicSections(content string, ws *Workspace) string {
	if ws == nil {
		return content
	}
	content = strings.ReplaceAll(content, "<!-- @date -->", time.Now().Format("2006-01-02"))
	if strings.Contains(content, "<!-- @files -->") {
		content = strings.ReplaceAll(content, "<!-- @files -->", workspaceFileTree(ws))
	}
	if strings.Contains(content, "<!-- @git-status -->") {
		content = strings.ReplaceAll(content, "<!-- @git-status -->", workspaceGitStatus(ws))
	}
	return content
}

// workspaceFileTree returns a newline-separated list of all non-hidden files
// and directories in the workspace, suitable for embedding in a system prompt.
func workspaceFileTree(ws *Workspace) string {
	var lines []string
	filepath.WalkDir(ws.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(ws.Root, path)
		if rel == "." {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			lines = append(lines, rel+"/")
		} else {
			lines = append(lines, rel)
		}
		return nil
	})
	if len(lines) == 0 {
		return "(empty workspace)"
	}
	return strings.Join(lines, "\n")
}

// workspaceGitStatus runs "git status --short" in the workspace root and
// returns the output. Returns "(not a git repository)" if git is unavailable
// or the directory is not tracked by git.
func workspaceGitStatus(ws *Workspace) string {
	cmd := exec.Command("git", "status", "--short")
	cmd.Dir = ws.Root
	out, err := cmd.Output()
	if err != nil {
		return "(not a git repository)"
	}
	result := strings.TrimRight(string(out), "\n")
	if result == "" {
		return "(nothing to commit, working tree clean)"
	}
	return result
}
