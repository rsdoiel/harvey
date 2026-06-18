package harvey

import (
	"fmt"
	"io"
)

/** HelpTopicsText returns a formatted index of all help topics with one-line
 * descriptions, suitable for printing to stdout or an io.Writer.
 *
 * Returns:
 *   string — formatted topic listing.
 *
 * Example:
 *   fmt.Print(HelpTopicsText())
 */
func HelpTopicsText() string {
	return `Available help topics — type 'harvey --help TOPIC' or '/help TOPIC' for full details:

  attach          Attach a file (image, PDF, or text) to the next turn
  builtin-tools   Built-in tools the LLM can invoke (read, write, run, search…)
  clear           Clear conversation history
  context         Conversation context and token budget management
  editing         Keyboard shortcuts and line-editing keybindings
  file-tree       Display the workspace directory tree
  files           List files in the workspace or a subdirectory
  format          Output format options (markdown, plain, code blocks)
  getting-started Installation walkthrough and first session guide
  git             Read-only git commands injected into context
  hint            Inject a one-off hint into the LLM context
  inspect         Inspect RAG stores, memory records, and model details
  kb              Knowledge base — experiments, observations, and concepts
  learn           Overview of Harvey's three-silo memory architecture
  llamafile       Single-file runnable model backends (llamafile)
  loop            Repeat a prompt or command on a timed interval
  memory          Experience memory — mine, list, show, flag, forget
  ollama          Local Ollama service control and model management
  pdf-tools       PDF extraction tools (requires poppler)
  pipeline        Multi-step pipelines chaining prompts and commands
  plan            Create and track multi-step work plans
  rag             Retrieval-augmented generation with named knowledge stores
  read            Read workspace files into conversation context
  read-dir        Read a directory tree into conversation context
  read-pdf        Extract and inject PDF text (requires poppler)
  record          Session recording in Fountain screenplay format
  rename          Rename a file in the workspace
  routing         Route prompts to remote model endpoints via @mention
  run             Run a shell command and inject its output into context
  search          Search workspace files by regex pattern
  security        Safe mode, permissions, audit log, and threat model
  session         Session management — list, resume, and replay sessions
  skill-set       Named bundles of skills for specific workflows
  skills          Custom skills — extend Harvey with SKILL.md files
  status          Show backend, workspace, model, and session status
  summarize       Compact long conversation history (alias: compact)
  write           Write the last reply or a code block to a file

  Aliases: audit, permissions, safe, safemode → security
           compact → summarize    edit, keys → editing
           filetree → file-tree   install, setup → getting-started
           knowledge → kb         pdf, pdftools → pdf-tools
           profile, recall → memory   readdir → read-dir
           recording → record     route → routing
           skill → skills         tools, builtins → builtin-tools

`
}

/** PrintHelpTopic writes the help guide for the named topic to w, substituting
 * appName, version, releaseDate, and releaseHash into man-page-style headers.
 * Pass empty strings when version metadata is not needed (e.g. inside the REPL).
 *
 * Parameters:
 *   w           (io.Writer) — destination writer.
 *   topic       (string)    — topic name or alias (case-insensitive).
 *   appName     (string)    — binary name, e.g. "harvey".
 *   version     (string)    — semver string, e.g. "0.0.12".
 *   releaseDate (string)    — ISO date, e.g. "2026-06-17".
 *   releaseHash (string)    — short git hash.
 *
 * Returns:
 *   bool — true if the topic was recognized, false if unknown.
 *
 * Example:
 *   ok := PrintHelpTopic(os.Stdout, "rag", "harvey", Version, ReleaseDate, ReleaseHash)
 *   if !ok {
 *       fmt.Fprintln(os.Stderr, "unknown topic")
 *   }
 */
func PrintHelpTopic(w io.Writer, topic, appName, version, releaseDate, releaseHash string) bool {
	f := func(text string) {
		fmt.Fprint(w, FmtHelp(text, appName, version, releaseDate, releaseHash))
	}
	switch topic {
	case "attach":
		f(AttachHelpText)
	case "builtin-tools", "tools", "builtins":
		f(BuiltinToolsHelpText)
	case "clear":
		f(ClearHelpText)
	case "compact", "summarize":
		f(SummarizeHelpText)
	case "context":
		f(ContextHelpText)
	case "editing", "edit", "keybindings", "keys":
		f(EditingHelpText)
	case "file-tree", "filetree":
		f(FileTreeHelpText)
	case "files":
		f(FilesHelpText)
	case "format":
		f(FormatHelpText)
	case "getting-started", "gettingstarted", "install", "setup":
		fmt.Fprint(w, GettingStartedHelpText)
	case "git":
		f(GitHelpText)
	case "hint":
		f(HintHelpText)
	case "inspect":
		f(InspectHelpText)
	case "kb", "knowledge", "knowledge-base":
		f(KBHelpText)
	case "learn", "memory-overview":
		f(LearnHelpText)
	case "llamafile":
		f(LlamafileHelpText)
	case "loop":
		f(LoopHelpText)
	case "memory", "profile", "recall":
		f(MemoryHelpText)
	case "ollama":
		f(OllamaHelpText)
	case "pdf-tools", "pdftools", "pdf":
		fmt.Fprint(w, PDFToolsHelpText)
	case "pipeline":
		f(PipelineHelpText)
	case "plan":
		f(PlanHelpText)
	case "rag":
		f(RagHelpText)
	case "read":
		f(ReadHelpText)
	case "read-dir", "readdir":
		f(ReadDirHelpText)
	case "read-pdf", "readpdf":
		f(ReadPDFHelpText)
	case "record", "recording":
		f(RecordHelpText)
	case "rename":
		f(RenameHelpText)
	case "routing", "route", "router":
		f(RoutingHelpText)
	case "run":
		f(RunHelpText)
	case "search":
		f(SearchHelpText)
	case "security", "safemode", "safe", "safe-mode", "permissions", "audit":
		f(SecurityHelpText)
	case "session", "sessions":
		f(SessionHelpText)
	case "skill-set", "skillset", "skill-sets":
		f(SkillSetHelpText)
	case "skills", "skill":
		f(SkillsHelpText)
	case "status":
		f(StatusHelpText)
	case "write":
		f(WriteHelpText)
	default:
		return false
	}
	return true
}
