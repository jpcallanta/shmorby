# How Shmorby Works — Architecture & Workflow
---

## 1. What Is Shmorby?

Shmorby is an **AI sysadmin agent** — not a coding agent. It operates
infrastructure via shell, SSH, sudo, and AWS CLI commands.
Provider-agnostic: swap between Ollama (local/free), OpenAI, OpenRouter,
or OpenCode Zen with a config change.

**Key distinction** from coding agents (OpenCode, Claude Code, Cursor):
- Shell-first, not file-edit-first
- Operational scope (hosts, services, environments), not a git worktree
- Artifacts are scripts and config files, not application code
- Workflow reads as a sysadmin runbook, not an IDE session

---

## 2. Layered Architecture

```
  TUI (bubbletea)  ────  stdin REPL (--no-tui)
        │
        ▼
  Agent loop + session ◀────▶ LLM provider registry
        │                           │
        │                    ┌──────┴──────┐
        │                    │             │
        │               OpenAI      OpenRouter
        │               Ollama       OpenCode Zen
        │
        ├── agent mode (operate | diagnose)
        ├── scope context (SCOPE.md + instructions)
        ├── memory (vector similarity + SQLite metadata)
        ├── context compressor (token estimation + offload)
        └── permissions (granular rules + presets)
        │
        ▼
  Tool runner (shell | ssh | sudo | aws)
```

### Package map

| Package | Role |
|---------|------|
| `cmd/shmorby` | CLI entrypoint (Cobra) |
| `internal/config` | Layered YAML merge |
| `internal/xdg` | Cross-platform path resolution |
| `internal/llm` | Provider interface + backends |
| `internal/agent` | Loop, modes, prompts, step cap, runtime config overrides (`/set`), no-TUI stdout formatting |
| `internal/tools` | Registry, schemas, permissions |
| `internal/session` | Message history |
| `internal/scope` | Load SCOPE.md + instructions |
| `internal/tui` | Bubbletea TUI model, views, styles |
| `internal/memory` | SQLite + chromem-go vector store |
| `internal/context` | Token estimation, compression, offload |
| `internal/audit` | Permission decision audit trail |

---

## 3. The Agent Loop

The core loop (one "turn" per user message):

1. **Append user message** to session history
2. **Estimate tokens** — if over model's context threshold, compress
   or offload
3. **Inject context** — scope docs, memory entries, agent prompt
4. **LLM call** — with tools filtered by agent mode + permissions
5. **Execute tool calls** — shell, ssh, sudo, aws (up to 20 iterations)
6. **Append results**, goto step 2 if more tool calls
7. **Text-only reply** — print; await next input
8. **Tool error** — return structured error to model; don't crash
9. **Step cap** — inject summary instruction, force final reply

---

## 4. Agent Modes

| Mode | Tools | Use case |
|------|-------|----------|
| **operate** (default) | shell, ssh, sudo*, aws* | Full infra management |
| **diagnose** | shell (read-only guard), ssh | Inspection only; no mutations |

*gated by config permissions

Switch via `/agent operate`, `/agent diagnose`, or Tab/Shift+Tab in TUI.

**Read-only guard**: blocks `rm`, `mv`, `dd`, `mkfs`, package
install/remove, systemctl start/stop, and redirects to `/etc`.

**Runtime config overrides**: the `/set` command modifies config at runtime
and propagates changes to affected components (provider, model, memory
auto-capture, tool timeouts, logging level, TUI theme, permission
presets/rules, context compression mode). Changes are applied
immediately and reflected in the `/help` parameter listing.

---

## 5. Sysadmin Workflow (prompt guidance)

The model is prompted to follow this runbook pattern:

1. **Discover** — `systemctl`, `ss`, `df`, package managers, cloud
   describe/list
2. **Plan** — state intent; list steps; call out blast radius
3. **Execute** — one logical step per tool call
4. **Verify** — health checks, logs, ports, `curl`, AWS describe
5. **Document** — append notes to a runbook file if asked

---

## 6. Permission System

Three layers, evaluated in order:

| Layer | Example |
|-------|---------|
| Tool-level | `shell: allow`, `sudo: ask`, `aws: ask` |
| Command rules (glob) | `match: "rm -rf /" ⇒ deny` |
| Built-in presets | `destructive`, `service`, `package`, `network`, `user`, `ssh`, `aws`, `sudo` |

**Interactive prompts**: tools with `ask` level show inline `y/n/a` in
TUI. `a` allows all for this turn. Configurable
`interactive: true/false`.

**Audit trail**: JSON-lines log at
`$XDG_DATA_HOME/shmorby/audit.log` (Unix) / `%LOCALAPPDATA%\shmorby\audit.log` (Windows) with timestamp, tool, command,
matched rule, decision, reason.

---

## 7. Memory System

Two layers:

| Layer | Backend | Purpose |
|-------|---------|---------|
| Session | In-memory message list | Current REPL session |
| Persistent | SQLite + chromem-go vectors | Cross-session retrieval |

**Episodic memory**: every tool execution is optionally persisted with
command, result, exit code, tags. Embedding via Ollama or OpenAI.

**Memory-aware loop**: before each LLM call, retrieve top-K relevant
entries and inject as context. Reduces calls for repeated tasks.

**Commands**: `/memory`, `/memory search <query>`,
`/memory forget <id>`, `/memory clear`, `/memory stats`.

---

## 8. Context Compression

Evaluated before every LLM call:

1. Get model's `ContextWindow` (API-fetched at startup)
2. Estimate token count (tiktoken or heuristic)
3. If over `ContextWindow × threshold` (default 80%): compress

**Two-phase compression**:
- Phase 1: Summarize large tool outputs (keep exit code, first/last N
  lines, errors)
- Phase 2: Collapse old message pairs into `[compressed]` summaries

**Offloading**: before summarization, full messages are saved to SQLite
memory for RAG retrieval.

**Modes**: `auto`, `aggressive` (60%), `conservative` (90%), `off`.
`auto` adapts to model size.

**Visibility**: status bar shows `ctx: 42k/128k (compressed 3x)`.

---

## 9. LLM Providers

Single `Provider` interface, 4 backends:

| Provider | Auth | Streaming | Embeddings |
|----------|------|-----------|------------|
| Ollama | None (local) | JSON-lines | `nomic-embed-text` |
| OpenAI | `openai.api_key` in YAML | SSE | `text-embedding-3-small` |
| OpenRouter | `openrouter.api_key` in YAML | SSE | — |
| OpenCode Zen | `opencode_zen.api_key` in YAML | SSE | — |

**Model info resolution** (no hardcoded context windows):
1. Provider API call (live fetch)
2. Config override (`models.<name>.context_window`)
3. Fallback 8192

---

## 10. Configuration Layering

Later wins:

1. `/etc/shmorby/config.yaml` (Unix) / `%ProgramData%\shmorby\config.yaml` (Windows) — optional
2. `~/.config/shmorby/config.yaml` (Unix) / `%APPDATA%\shmorby\config.yaml` (Windows)
3. `--config` flag
4. `./shmorby.yaml` in cwd
5. CLI flags (`--provider`, `--model`, `--agent`)

Secrets via `api_key` fields in YAML.

---

## 11. Runtime Config Overrides (`/set`)

The `/set <param> <value>` command modifies config at runtime and
propagates changes to live components without restarting.

**Overrideable parameters:**

| Param | Type | Example |
|-------|------|---------|
| `provider` | string | `/set provider openai` |
| `model` | string | `/set model gpt-4o` |
| `agent.default` | string | `/set agent.default diagnose` |
| `tools.timeout` | int | `/set tools.timeout 60` |
| `tools.sudo.enabled` | bool | `/set tools.sudo.enabled true` |
| `tools.aws.enabled` | bool | `/set tools.aws.enabled true` |
| `permission.shell` | string | `/set permission.shell deny` |
| `permission.ssh` | string | `/set permission.ssh allow` |
| `permission.sudo` | string | `/set permission.sudo deny` |
| `permission.aws` | string | `/set permission.aws deny` |
| `permission.interactive` | bool | `/set permission.interactive false` |
| `permission.presets` | string list | `/set permission.presets destructive,service` |
| `memory.auto_capture` | bool | `/set memory.auto_capture false` |
| `context.mode` | string | `/set context.mode aggressive` |
| `log.level` | string | `/set log.level debug` |
| `tui.fullscreen` | bool | `/set tui.fullscreen true` |
| `tui.theme` | string | `/set tui.theme catppuccin-latte` |

**Propagation**: `ConfigOverrider` (in `internal/agent/setter.go`) writes
the new value into the shared `config.Config` struct and calls component
setters (provider swap, log level, memory toggle, etc.) so changes take
effect immediately. The updated state is reflected in the `/help` overlay's
CONFIG PARAMETERS section.

**Restart-required**: some changes (e.g. switching between API-based
providers and Ollama) recreate the LLM provider on the fly; others like
TUI theme are purely cosmetic and apply instantly.

---

## 12. TUI Design (bubbletea)

Bottom-anchored layout, inspired by Claude Code CLI and OpenCode:

```
  [scrollable output pane — agent replies, tool output]
  ──── 💭 thinking (5s · 412 tokens) ────────────
  [collapsible thinking / log section]
  ────────────────────────────────────────────────
  ❯ deploy nginx reverse proxy for app on :8080
  ────────────────────────────────────────────────
    agent: operate │ provider: ollama │ model: llama3.2
    /help  /quit  /reset  /model  /agent  /scope  /memory
```

**Key TUI features**:
- Markdown rendering via glamour (syntax-highlighted code, styled headers)
- Collapsible thinking block (`ctrl+t`)
- Collapsible log section (`ctrl+l`) — slog entries in viewport
- Slash-command autocomplete, command palette (`ctrl+p`)
- Reverse-i-search (`ctrl+r`) through input history
- Leader key system (`ctrl+x` → which-key popup)
- @-reference autocomplete (hostnames, services, paths)
- !-prefixed raw shell commands (bypass LLM)
- Multi-session tab bar
- Scroll acceleration, selection copy/paste
- Catppuccin themes (mocha, latte, frappe, macchiato, minimal)
- Fullscreen mode (no flicker) vs `--no-tui` plain REPL
- `--no-tui` REPL features ANSI markdown rendering, streaming
  spinners, and structured permission prompts via `internal/agent/stdout.go`

---

## 13. Tools

Ops-oriented only — no read/edit/write/grep:

| Tool | Args | Notes |
|------|------|-------|
| shell | command, cwd, timeout | Default 120s, truncate >64KiB |
| ssh | host, user, command | Key-based, `BatchMode=yes` |
| sudo | command | Requires `sudo -n`, gated by config |
| aws | args array | Respects AWS env, gated by config |

## 14. Key Design Decisions

- **Go over Python/Node**: performance for shell execution, single
  binary, stdlib-first
- **Shell over file tools**: models a sysadmin's actual workflow
- **Provider-agnostic via interface**: swap LLMs without agent logic
  changes
- **No mandatory project root**: run from anywhere; `--scope-file` for
  context
- **API-fetched model info**: context windows adapt to the actual model,
  no hardcoding
- **Local-first memory**: SQLite + chromem-go, no external services, no
  data leaks
- **Glob-based permission rules**: familiar pattern, expressive without
  regex complexity


