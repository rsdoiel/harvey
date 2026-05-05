
# Harvey `/skill-set` Design (Ollama-Centric)

## Directory Structure

```
agents/
├── skills/               # Skill implementations (e.g., `lint`, `test`)
└── skill-sets/           # Skill set YAML files (e.g., `code-review.yaml`)
```

## YAML Structure (Skill Set)

```yaml
name: code-review
description: Tools for code quality
skills:
  - trigger: /lint
    prompt: "Analyze this {language} code for PEP8/compliance: {code}"
  - trigger: /test
    prompt: "Write tests for this {language} function: {code}"
metadata:
  version: 1.0
  dependencies: [pylint, flake8]
```

## Commands

| Command               | Action                                                                                     |
|-----------------------|--------------------------------------------------------------------------------------------|
| `/skill-set load <name>` | Loads `<name>.yaml`. Rejects if triggers are missing/duplicate. Calculates token count via Ollama API. |
| `/skill-set unload`     | Clears the current skill set from context.                                                |
| `/skill-set list`       | Lists all YAML files in `agents/skill-sets/`.                                             |
| `/skill-set create <name>` | Creates a new empty YAML file for `<name>`.                                              |
| `/skill-set delete <name>` | Deletes `<name>.yaml`.                                                                     |

## Validation Rules

- **Missing Skills**: Reject load if any `trigger` in the YAML doesn’t exist in `agents/skills/`.
- **Duplicate Triggers**: Reject load if any `trigger` is duplicated in the YAML.
- **Token Count**: Warn if the total tokens for the skill set exceed the model’s context window.

## Tokenization

- Use Ollama’s `/api/tokenize` endpoint for all models (pulled or local).
- Example API call:
  ```bash
  curl http://localhost:11434/api/tokenize -d '{"model": "phi:2.7b", "content": "Your prompt here"}'
  ```

## Go Implementation Notes

- Use `gopkg.in/yaml.v3` for YAML parsing.
- Use `net/http` to call Ollama’s API for tokenization.
- Validate triggers against `agents/skills/` before loading.
- need to figure out how to handle llamafile and tokenization counte
