# Harvey Security Model

Harvey is a **local-first** agent. The primary threat model is protecting
your local machine and data, not network confidentiality. This document
describes the protections in place, known limitations, and how to report
security issues.

---

## Core protections

### Workspace sandboxing

All file operations (read, write, delete, list) are constrained to the
workspace root. Path traversal via `../`, symlinks that escape the root, and
absolute paths outside the workspace are rejected at `resolveWorkspacePath()`
using `filepath.EvalSymlinks` before any comparison.

### Sensitive file blocking

The following file patterns are blocked from tool access regardless of
workspace position:

| Pattern | Reason |
|---------|--------|
| `.env`, `.env.*` | Environment variable files |
| `.pem`, `.key`, `.p12`, `.pfx` | TLS/crypto keys |
| `id_rsa`, `id_ed25519`, `authorized_keys` | SSH keys |
| `harvey.yaml` | Runtime configuration (API keys, permissions) |
| `agents/` subtree | Skills, sessions, knowledge base |

### Shell injection prevention

The `!` command and `/run` use `exec.Command()` directly — no shell
interpreter is invoked. `parseCommandLine()` rejects shell metacharacters
(`;`, `|`, `&`, `>`, `<`, `` ` ``, `$`, `(`, `)`, `{`, `}`, `[`, `]`)
before execution.

### API key filtering

The following environment variables are stripped from all child processes
(skills, `/run`, `/git`):

`ANTHROPIC_API_KEY`, `COHERE_API_KEY`, `DEEPSEEK_API_KEY`,
`GEMINI_API_KEY`, `GOOGLE_API_KEY`, `GROQ_API_KEY`, `MISTRAL_API_KEY`,
`OPENAI_API_KEY`, `PERPLEXITY_API_KEY`

Error messages reference variable names only, never their values.

### Safe Mode

`/safemode on` restricts `/run` and `!` to a configurable command allowlist
(default: `ls`, `cat`, `grep`, `head`, `tail`, `wc`, `find`, `stat`, `jq`,
`htmlq`, `bat`, `batcat`). Manage with `/safemode allow COMMAND` and
`/safemode deny COMMAND`. Safe Mode state and the allowlist persist across
sessions in `agents/harvey.yaml`.

### Permissions system

Path-prefix permissions stored in `agents/harvey.yaml` gate file
operations. Managed with `/permissions set PATH PERMS`. Supported
permissions: `read`, `write`, `exec`, `delete`.

### Audit logging

An in-memory ring buffer (1000 events) records all commands, file
operations, and skill runs with a status of `allowed` or `denied`. View with
`/audit show`, clear with `/audit clear`. The buffer does not persist across
sessions — no sensitive history is written to disk.

### Configurable timeouts

`run_timeout` (default 5 min) in `agents/harvey.yaml` caps shell command
execution via `exec.CommandContext`, preventing runaway processes.

---

## Network security

Harvey connects to:

- **Local LLM servers** (`ollama://`, `llamafile://`, `llamacpp://`) over
  HTTP by default. If the server is on a remote host, prompts and model
  responses travel unencrypted. Use a VPN or SSH tunnel on untrusted
  networks, or configure the server to support TLS and register the endpoint
  with `https://`. Harvey warns when a non-localhost `http://` URL is
  registered.

- **Cloud providers** (`anthropic://`, `deepseek://`, `gemini://`,
  `mistral://`, `openai://`) — all cloud SDKs enforce HTTPS internally.
  API keys are read from environment variables and are never written to disk
  by Harvey.

---

## Known limitations

| Limitation | Notes |
|-----------|-------|
| No OS-level sandboxing | Harvey uses pure Go isolation. Linux namespaces or seccomp require OS support. True isolation requires containers. |
| Audit log is in-memory | Lost when Harvey exits. No persistent audit trail across sessions. |
| No rate limiting | No per-turn cap on tool invocations from the model. |
| No tool input size limits | No maximum length validation on tool string inputs. |

---

## Reporting security issues

Report vulnerabilities via GitHub Issues:
<https://github.com/rsdoiel/harvey/issues>

Include: a description of the issue, steps to reproduce, and your
assessment of severity.
