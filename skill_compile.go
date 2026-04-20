package harvey

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// compileMetaPrompt is sent to the LLM before the skill body to instruct it
// to produce exactly two fenced code blocks — compiled.bash and compiled.ps1.
const compileMetaPrompt = `You are a code generator. The user will provide you with a SKILL.md file.
Your task is to produce two shell scripts that implement the skill's instructions
for automated local execution:

1. A bash script for Linux/macOS/BSD (scripts/compiled.bash)
2. A PowerShell script for Windows (scripts/compiled.ps1)

The scripts will be called with no arguments. Context is provided via environment
variables:
  HARVEY_PROMPT      — the user's original prompt text
  HARVEY_WORKDIR     — absolute path to the workspace root directory
  HARVEY_MODEL       — name of the current LLM model
  HARVEY_SESSION_ID  — current session ID (string)

Requirements for both scripts:
- Read HARVEY_PROMPT (or $env:HARVEY_PROMPT in PowerShell) as the primary input.
- Work with files relative to HARVEY_WORKDIR.
- Print results to stdout; print errors to stderr.
- Exit 0 on success, non-zero on failure.
- Do not require interactive input — run fully non-interactively.
- Do not call external LLMs or APIs unless the SKILL.md instructions explicitly
  require it.

IMPORTANT bash rules — these are required, do not deviate:
- Use ${HARVEY_PROMPT:-} (not $null, not ${HARVEY_PROMPT:-$null}) to read an
  optional env var with an empty-string default. $null does not exist in bash.
- Variables are checked with: if [ -n "$HARVEY_PROMPT" ]; then ... fi
- Never use PowerShell syntax ($env:VAR, $null, Write-Host) inside bash scripts.

Output format — you MUST produce exactly two fenced code blocks and NOTHING ELSE
outside them. No explanation, no preamble, no trailing text.

` + "```" + `bash:scripts/compiled.bash
#!/usr/bin/env bash
set -euo pipefail
prompt=${HARVEY_PROMPT:-}
if [ -z "$prompt" ]; then
    echo "Hello World!" >&2
    exit 1
fi
echo "Hello World!"
` + "```" + `

` + "```" + `powershell:scripts/compiled.ps1
$prompt = $env:HARVEY_PROMPT
if (-not $prompt) {
    Write-Error "HARVEY_PROMPT is not set"
    exit 1
}
Write-Output "Hello World!"
` + "```" + `

The above is only an EXAMPLE structure — replace the logic with what the skill
actually requires. The skill instructions follow:

`

/** CompileSkill sends the skill body to the LLM with a meta-prompt that instructs
 * it to produce compiled.bash and compiled.ps1 scripts. The generated scripts are
 * written to the skill's scripts/ directory, which is created if absent.
 *
 * If the LLM response does not contain both expected code blocks, an error is
 * returned and no files are written.
 *
 * Parameters:
 *   ctx    (context.Context) — for LLM call cancellation.
 *   client (LLMClient)       — the connected LLM backend.
 *   skill  (*SkillMeta)      — skill to compile; skill.Path and skill.Body must be set.
 *   out    (io.Writer)       — receives streamed LLM output during generation.
 *
 * Returns:
 *   error — on LLM failure, missing code blocks, or file write failure.
 *
 * Example:
 *   err := CompileSkill(ctx, agent.Client, skill, os.Stdout)
 *   if err != nil { log.Fatal(err) }
 */
func CompileSkill(ctx context.Context, client LLMClient, skill *SkillMeta, out io.Writer) error {
	prompt := compileMetaPrompt + skill.Body
	messages := []Message{{Role: "user", Content: prompt}}

	var buf strings.Builder
	mw := io.MultiWriter(out, &buf)
	if _, err := client.Chat(ctx, messages, mw); err != nil {
		return fmt.Errorf("compile skill %q: LLM error: %w", skill.Name, err)
	}

	response := buf.String()

	// Extract the two required code blocks from the LLM response.
	blocks := findTaggedBlocks(response)
	bashContent, ps1Content, err := extractCompiledBlocks(blocks)
	if err != nil {
		return fmt.Errorf("compile skill %q: %w", skill.Name, err)
	}

	// Write scripts to <skilldir>/scripts/.
	scriptsDir := filepath.Join(filepath.Dir(skill.Path), "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		return fmt.Errorf("compile skill %q: create scripts dir: %w", skill.Name, err)
	}

	bashPath := filepath.Join(scriptsDir, "compiled.bash")
	if err := os.WriteFile(bashPath, []byte(bashContent), 0o755); err != nil {
		return fmt.Errorf("compile skill %q: write compiled.bash: %w", skill.Name, err)
	}

	ps1Path := filepath.Join(scriptsDir, "compiled.ps1")
	if err := os.WriteFile(ps1Path, []byte(ps1Content), 0o644); err != nil {
		return fmt.Errorf("compile skill %q: write compiled.ps1: %w", skill.Name, err)
	}

	return nil
}

// extractCompiledBlocks finds the bash and PS1 blocks in the LLM-generated
// tagged block list. Both are required; an error names the first missing one.
func extractCompiledBlocks(blocks []taggedBlock) (bash, ps1 string, err error) {
	for _, b := range blocks {
		switch {
		case strings.HasSuffix(b.path, "compiled.bash"):
			bash = b.content
		case strings.HasSuffix(b.path, "compiled.ps1"):
			ps1 = b.content
		}
	}
	if bash == "" {
		return "", "", fmt.Errorf("LLM did not produce scripts/compiled.bash")
	}
	if ps1 == "" {
		return "", "", fmt.Errorf("LLM did not produce scripts/compiled.ps1")
	}
	return bash, ps1, nil
}
