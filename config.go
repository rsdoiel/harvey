package harvey

import "os"

// Config holds Harvey's runtime configuration.
type Config struct {
	SystemPrompt  string // contents of HARVEY.md, injected as the system prompt
	OllamaURL     string // Ollama base URL (default: http://localhost:11434)
	OllamaModel   string // currently selected Ollama model
	PublicAIURL   string // publicai.co base URL (default: https://api.publicai.co/v1)
	PublicAIKey   string // API key read from PUBLICAI_API_KEY
	PublicAIModel string // model name (default: abertus)
}

// DefaultConfig returns a Config populated with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		OllamaURL:     "http://localhost:11434",
		PublicAIURL:   "https://api.publicai.co/v1",
		PublicAIKey:   os.Getenv("PUBLICAI_API_KEY"),
		PublicAIModel: "abertus",
	}
}

// LoadHarveyMD reads HARVEY.md from the current directory and returns its
// contents. Returns an empty string if the file does not exist.
func LoadHarveyMD() string {
	data, err := os.ReadFile("HARVEY.md")
	if err != nil {
		return ""
	}
	return string(data)
}
