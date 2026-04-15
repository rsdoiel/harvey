package harvey

import "os"

/** Config holds Harvey's runtime configuration.
 *
 * Fields:
 *   WorkDir       (string) — root directory Harvey is allowed to read/write; defaults to ".".
 *   SystemPrompt  (string) — contents of HARVEY.md, injected as the system prompt.
 *   OllamaURL     (string) — Ollama base URL (default: http://localhost:11434).
 *   OllamaModel   (string) — currently selected Ollama model.
 *   PublicAIURL   (string) — publicai.co base URL.
 *   PublicAIKey   (string) — API key read from PUBLICAI_API_KEY env var.
 *   PublicAIModel (string) — model name (default: abertus).
 *
 * Example:
 *   cfg := DefaultConfig()
 *   cfg.WorkDir = "/home/user/myproject"
 */
type Config struct {
	WorkDir          string // workspace root; all file I/O is constrained to this tree
	CurrentProjectID int64  // ID of the active knowledge-base project (0 = none)
	SystemPrompt  string // contents of HARVEY.md, injected as the system prompt
	OllamaURL     string // Ollama base URL (default: http://localhost:11434)
	OllamaModel   string // currently selected Ollama model
	PublicAIURL   string // publicai.co base URL (default: https://api.publicai.co/v1)
	PublicAIKey   string // API key read from PUBLICAI_API_KEY
	PublicAIModel string // model name (default: abertus)
}

/** DefaultConfig returns a Config populated with sensible defaults. WorkDir
 * defaults to "." (the process working directory at startup).
 *
 * Returns:
 *   *Config — configuration with default values pre-filled.
 *
 * Example:
 *   cfg := DefaultConfig()
 *   fmt.Println(cfg.OllamaURL) // "http://localhost:11434"
 */
func DefaultConfig() *Config {
	return &Config{
		WorkDir:       ".",
		OllamaURL:     "http://localhost:11434",
		PublicAIURL:   "https://api.publicai.co/v1",
		PublicAIKey:   os.Getenv("PUBLICAI_API_KEY"),
		PublicAIModel: "abertus",
	}
}

/** LoadHarveyMD reads HARVEY.md from the current directory and returns its
 * contents. Returns an empty string if the file does not exist.
 *
 * Returns:
 *   string — contents of HARVEY.md, or "" if not found.
 *
 * Example:
 *   prompt := LoadHarveyMD()
 *   cfg.SystemPrompt = prompt
 */
func LoadHarveyMD() string {
	data, err := os.ReadFile("HARVEY.md")
	if err != nil {
		return ""
	}
	return string(data)
}
