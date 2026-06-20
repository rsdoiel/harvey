
# Harvey – A Terminal‑Based AI Interaction Layer

## What Harvey Is  

Harvey is an open‑source, cross‑platform terminal REPL (Read‑Eval‑Print Loop) that lets you interact with language models (LLMs) through a simple, tool‑rich command interface. Built primarily on Ollama with support for cloud providers via named routes, Harvey provides an interactive coding agent experience focused on local model execution.

## Why You Might Want to Use It  

| Feature | Benefit |
|---|---|
| **Unified Command Set** (`/read`, `/write`, `/run`, `/attach`, `/search`, `/git`, `/rag`, `/memory`, `/kb`, etc.) | Interact with models without remembering separate API calls. |
| **RAG Integration** | Inject context from local files, PDFs, or remote storage (S3, HTTP, HTTPS, SFTP, SCP) into your prompts. Create and query SQLite‑backed vector stores with `/rag` commands. |
| **Tool Calling & Schema Support** | Harvey provides built-in tools (read_file, write_file, run_command, etc.) with validated schemas for safe operation. |
| **Multi‑Model & Multi‑Backend Routing** | Dispatch prompts to specific models (`@model_name`) or route to different backends (Ollama, Anthropic, DeepSeek, Gemini, Mistral, OpenAI) via `/route add`. |
| **Session Recording (Fountain screenplay)** | Sessions can be recorded as Fountain‑compatible `.spmd` files using `/record start` or `--record` flag, enabling later review, summarization, or replay. |
| **Knowledge Base & Memory** | Persistent knowledge base (`agents/knowledge.db`) plus a unified memory system with rolling summaries and token budget tracking. |
| **Secure Safe Mode** | Execution is limited to a whitelisted set of shell commands; remote file reads enforce size limits and host‑key verification. API keys are filtered from child processes. |
| **Installer Scripts** | Installer scripts for Linux (x86_64/aarch64/armv7l), macOS, and Windows. |
| **Extensible Skill System** | Load specialized skills via `/skill load <name>` to add domain‑specific tooling. |

## Getting Started  

1. **Install Harvey** – Run the installer script for your OS (`installer.sh` for Linux/macOS, `installer.ps1` for Windows).  
2. **Launch the REPL**  

   ```bash
   harvey
   ```

   You’ll be greeted with a prompt like `harvey >`.  

3. **Basic Commands**  

   - **Read a file** – `/read <path>`.  
   - **Write output** – `/write <path>` (writes last assistant reply to a file).  
   - **Run shell commands safely** – `/run ls -l`.  
   - **Query the knowledge base** – `/kb search <text>`.  
   - **Attach a file for this turn** – `/attach docs/manual.pdf` (image, PDF text extraction, or plain text; not stored in RAG).  

4. **Using RAG**  

   ```bash
   /rag new my_store
   /rag use my_store
   ```

5. **Recording a Session**  

   Harvey can record sessions to Fountain files. To start recording:

   ```bash
   /record start
   ```

## Design Decisions (Three Core Principles)

1. **Tool‑First Architecture** – All user actions are exposed as first‑class *tools* (`read_file`, `write_file`, `run_command`, etc.). This makes the system extensible: new capabilities can be added by registering a tool with its schema, without touching core logic.

2. **Unified Memory & Knowledge Base** – Harvey separates three storage layers:
   - **RAG Store** (vector DB for fast semantic search).  
   - **Memory System** (rolling summary and token‑budget tracking).  
   - **Knowledge Base** (structured SQLite tables for experiments, concepts, observations).  
   A single `MemoryConfig` configuration drives budgeting and switching between them.

3. **Secure Safe Mode with Remote Protocol Support** – Execution is gated by a whitelist; remote file reads (`s3://`, `http://`, `https://`, `sftp://`, `scp://`) enforce strict size caps and host-key verification, preventing accidental data leakage or denial-of-service attacks.

---

### Next Steps  

- Run the installer (if you haven’t already).  
- Try a few commands to see tool responses:  

  ```bash
  harvey > /read LICENSE
  harvey > /kb status
  ```

- Explore available skills and load any that interest you  

Feel free to ask for more detailed examples or to tailor the document further!
