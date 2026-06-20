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
interpreter is invoked, so shell metacharacters (`;`, `|`, `&`, `>`, `<`,
`` ` ``, `$`, `(`, `)`, `{`, `}`, `[`, `]`) are passed as literal string
arguments rather than being interpreted. `parseCommandLine()` handles
quoted-string splitting only; it does not reject these characters.

### API key filtering

Child processes (skills, `/run`, `/git`) receive a **whitelist-filtered**
environment — only variables whose names match a small set of safe prefixes
(`PATH`, `HOME`, `USER`, `SHELL`, `TERM`, `LANG`, `LC_*`, `PWD`, `HARVEY_*`,
`OLLAMA_*`) are forwarded. Everything else is stripped.

As defence-in-depth, the following known sensitive names are also explicitly
denied before the whitelist is applied, so a future prefix change cannot
accidentally expose them:

`ANTHROPIC_API_KEY`, `COHERE_API_KEY`, `DEEPSEEK_API_KEY`,
`FIREWORKS_API_KEY`, `GEMINI_API_KEY`, `GOOGLE_API_KEY`, `GROQ_API_KEY`,
`HUGGINGFACE_TOKEN`, `MISTRAL_API_KEY`, `OPENAI_API_KEY`,
`PERPLEXITY_API_KEY`, `PUBLICAI_API_KEY`, `REPLICATE_API_KEY`,
`TOGETHER_API_KEY`, `XAI_API_KEY`, `AWS_ACCESS_KEY_ID`,
`AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN`, `SFTP_PASSWORD`,
`HTTP_BEARER_TOKEN`, `HTTP_BASIC_PASSWORD`

Error messages reference variable names only, never their values.

**`HARVEY_*` pass-through:** Harvey injects its own `HARVEY_*` session
variables (workspace path, model name, API base URL) into child processes so
that compiled skills can call the LLM API. Any `HARVEY_*` variable already
present in the parent shell's environment is also forwarded. Avoid storing
sensitive data in `HARVEY_`-prefixed variables.

### Safe Mode

`/safemode on` restricts `/run` and `!` to a configurable command allowlist
(default: `ls`, `cat`, `grep`, `head`, `tail`, `wc`, `find`, `stat`, `jq`,
`htmlq`, `bat`, `batcat`). Manage with `/safemode allow COMMAND` and
`/safemode deny COMMAND`. Safe Mode state and the allowlist persist across
sessions in `agents/harvey.yaml`.

**Safe Mode limitation:** the allowlist controls *which programs* can be
launched. It does not control *which files those programs can access*. An
allowed command such as `cat` or `grep` can still read files outside the
workspace boundary (e.g. `/etc/passwd`, `~/.ssh/id_rsa`). Use the
**Permissions system** (below) to gate file-tool access; use Safe Mode
to limit which programs the LLM can invoke via `/run`.

### Permissions system

Path-prefix permissions stored in `agents/harvey.yaml` gate file
operations. Managed with `/permissions set PATH PERMS`. Supported
permissions: `read`, `write`, `exec`, `delete`.

### Audit logging

A ring buffer (1000 events) records all commands, file operations, and skill
runs with a status of `allowed` or `denied`. When a workspace is present,
events are also appended to `agents/audit.jsonl` in NDJSON format so the
trail survives restarts. View with `/audit show`, check the log path with
`/audit status`, and clear the in-memory buffer with `/audit clear`.

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
| Audit log is append-only | `agents/audit.jsonl` grows without bound; no automatic rotation. |
| No rate limiting | No per-turn cap on tool invocations from the model. |
| No tool input size limits | No maximum length validation on tool string inputs. |

Directory walkers (`/read-dir`, `/search`, file-tree expansion, `grep_files` tool)
skip symbolic links entirely — they will not follow a symlink to read or list its
target.

---

## Reporting security issues

Report vulnerabilities privately via GitHub Security Advisories:
<https://github.com/rsdoiel/harvey/security/advisories/new>

Include: a description of the issue, steps to reproduce, and your
assessment of severity. Do not open a public GitHub Issue for a security
vulnerability until a coordinated fix is available.
