# How Shmorby Works — Architecture & Workflow
---

## 1. What Is Shmorby?

Shmorby is an **AI sysadmin agent** — primarily for operations, with an
opt-in `code` mode for file-level codebase work. It operates infrastructure
via shell, SSH, sudo, and AWS CLI commands. Provider-agnostic: swap
between Ollama (local/free), OpenAI, OpenRouter, or OpenCode Zen with a
config change.

**Key distinction** from coding agents (OpenCode, Claude Code, Cursor):
- Shell-first, not file-edit-first (default operate/diagnose/chat modes)
- Operational scope (hosts, services, environments), not a git worktree
- Artifacts are scripts and config files, not application code
- Workflow reads as a sysadmin runbook, not an IDE session

**Code mode** (`/agent code`): opt-in coding-agent mode that adds
`file_read`, `file_edit`, `file_write`, `find`, and `grep` tools for
reading, editing, writing, and searching codebases. Designed for software
development tasks like code review, refactoring, and bug fixes.

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
        ├── agent mode (operate | diagnose | chat | code)
        ├── scope context (SCOPE.md + instructions)
        ├── memory (vector similarity + SQLite metadata)
        ├── context compressor (token estimation + offload)
        └── permissions (granular rules + presets)
        │
        ▼
  Tool runner (shell | ssh | sudo | aws | find | task |
               websearch | webfetch | file_read | file_edit |
               file_write | grep | mcp*)
```

### Package map

| Package | Role |
|---------|------|
| `cmd/shmorby` | CLI entrypoint (Cobra) |
| `internal/config` | Layered YAML merge |
| `internal/xdg` | Cross-platform path resolution |
| `internal/llm` | Provider interface + backends |
| `internal/agent` | Agent loop, modes, prompts, `/set` overrides, stdout fmt |
| `internal/tools` | Registry, schemas, permissions, find/task, file tools, MCP |
| `internal/exec` | Unified Executor interface + OSExecutor (process-group isolation, context-aware pipe reads) |
| `internal/session` | Message history + SQLite persistence/resume |
| `internal/scope` | Load SCOPE.md + instructions |
| `internal/tui` | Bubbletea TUI model, views, styles |
| `internal/memory` | SQLite + chromem-go vector store |
| `internal/context` | Token estimation, compression, offload |
| `internal/audit` | SQLite audit store: runs, outputs, perms, subagents |
| `internal/redact` | Secret-pattern redaction for persisted output/args |
| `internal/fileread` | Size-limited file reads (10 MB) for config/scope |
| `internal/ledger` | Encrypted environment ledger (age + flock / LockFileEx) |
| `internal/health` | Self-diagnostics (`shmorby doctor`) + `Degraded` wrapper |
| `internal/xuuid` | Shared UUID v4 generator (crypto/rand) |

---

## 3. The Agent Loop

The core loop (one "turn" per user message):

1. **Append user message** to session history
2. **Estimate tokens** — if over model's context threshold, compress
   or offload
3. **Inject context** — scope docs, memory entries, agent prompt
4. **LLM call** — with tools filtered by agent mode + permissions
5. **Execute tool calls** — shell, ssh, sudo, aws, find, task,
   websearch, webfetch (up to 20 iterations)
6. **Append results**, goto step 2 if more tool calls
7. **Text-only reply** — print; await next input
8. **Tool error** — return structured error to model; don't crash
9. **Step cap** — inject summary instruction, force final reply

---

## 4. Agent Modes

| Mode | Tools | Use case |
|------|-------|----------|
| **operate** | shell/ssh/sudo/aws, find, task, web*, mcp* | Infra mgmt |
| **diagnose** | shell/ssh/sudo/aws, web* (read-only guard) | Inspection |
| **chat** | websearch*, webfetch* | Conversation |
| **code** | file_read/file_edit/file_write, find, grep, shell, task, web* | Codebase work |

*gated by config; sudo/aws/websearch/webfetch disabled by default;
mcp tools only when `mcp.servers` configured; code mode file tools
gated by `file_read`/`file_edit`/`file_write`/`grep` permissions

Switch via `/agent operate`, `/agent diagnose`, `/agent chat`,
`/agent code`, or Tab/Shift+Tab in TUI.

**Read-only guard** (shell, sudo, and ssh in diagnose mode): blocks `rm`,
`mv`, `dd`, `mkfs`, package install/remove, systemctl start/stop,
redirects to `/etc`, `chmod`/`chown` on sensitive paths, `eval`,
`$()`/backtick command substitution containing mutating verbs, `xargs`
piped to mutating verbs, `tee` to sensitive paths, and `curl`/`wget`
piped to a shell. The guard applies to `shell`, `sudo`, and `ssh`
tool invocations.

**Project root anchoring** (code mode): the directory shmorby is
launched from becomes the project root for file tools. File tools are
confined to the root: paths resolving outside it are rejected unless
matched by `code.allowed_patterns`, and symlinks are evaluated so `../`
+ symlink escapes cannot bypass the boundary. Blocked patterns (`.git/`,
`vendor/`, `node_modules/`, `.idea/`, `.vscode/`) are always rejected —
also without a project root, where the blocked-name check itself
resolves symlinks before deciding. New files and directories written by
`file_write` are created through the process umask (no hardcoded
world-readable mode); existing files keep their permissions.
`file_read` also refuses non-regular files (device nodes, FIFOs,
directories) and files above a 10 MiB cap to prevent OOM; `file_edit`
reads through a single handle and replaces via an atomic rename to
prevent TOCTOU corruption and symlink write-through. Override the root
with `code.workdir` in config.

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
| Tool-level | `shell: allow`, `sudo: ask`, `task: ask`, `mcp: ask` |
| Command rules (glob) | `match: "rm -rf /" ⇒ deny` |
| Built-in presets | `destructive`, `service`, `package`, `network`, `user`, `ssh`, `aws`, `sudo` |

**Interactive prompts**: tools with `ask` level show inline `y/n/a` in
TUI. `a` allows all for this turn. Configurable
`interactive: true/false`.

**Audit trail**: every tool run, permission decision, and subagent
dispatch is written to a SQLite database at
`$XDG_DATA_HOME/shmorby/audit.db` (default
`~/.local/share/shmorby/audit.db` on Unix,
`%LOCALAPPDATA%\shmorby\audit.db` on Windows)
with tool, args, exit code, duration, captured stdout/stderr (capped by
`audit.output_capture_max_bytes`, default 64 KiB), and matched rule +
decision + reason for permission checks. Secrets (AWS keys, Bearer
tokens, API keys, private keys, Slack/Stripe/npm/GitLab tokens, JWTs,
DB connection strings, etc.) are redacted from output and args
before storage via `internal/redact`. Query it with the `audit` CLI
subcommand: `audit list`, `audit get <id>`, `audit session <id>`,
`audit export --format json|csv`, `audit vacuum`, `audit stats`.
Retention is governed by `audit.retention_days` (default 365).

---

## 7. Memory System

Two layers:

| Layer | Backend | Purpose |
|-------|---------|---------|
| Session | In-memory list (max 1000 messages) | Current REPL session |
| Persistent | SQLite + chromem-go vectors | Cross-session retrieval |

**Episodic memory**: every tool execution is optionally persisted with
command, result, exit code, tags. Embedding via Ollama or OpenAI.
Persisted results are run through `internal/redact` so credentials in
tool output never land in the memory store.

**Memory-aware loop**: before each LLM call, retrieve top-K relevant
entries and inject as context. Reduces calls for repeated tasks.
Injected entries are wrapped in header/footer text marking them as
untrusted reference data, since tool output can be attacker-influenced
and must never be read as instructions.

**Token budget**: `memory.context_budget` (default 0 = unlimited) caps
the injected memory context to a token estimate, dropping the
lowest-ranked entries to fit. Extractive outcome summaries (exit code +
result head) are stored per entry so retrieval matches on results, not
just commands.

**Commands**: `/memory`, `/memory search <query>`,
`/memory forget <id>`, `/memory clear`, `/memory stats`.

---

## 7b. Session Persistence

Conversations are persisted continuously to SQLite
(`internal/session/store.go`, `sessions.db` under the xdg data dir)
so they survive exits and crashes and can be resumed. Same storage
idioms as audit/memory: `mattn/go-sqlite3`, WAL, `PRAGMA
user_version` migration ladder (newer DBs refuse to open), 0600 file
mode.

Two tables: `sessions` (metadata: title, directory, agent mode,
provider, model, parent link, timestamps, `last_seq`) and
`session_messages` (PK `(session_id, seq)`, role, content, tool
correlation fields, JSON tool calls).

**Write path.** `Session.BindStore()` turns every append into a
write-through inside the session mutex: the agent loop flushes each
completed turn via `AppendMessages`, so persistence granularity
matches the turn granularity the model sees. A session row is
created lazily on the first *user* message (no empty-session
clutter) with the title derived from that message (~60 chars). Every
write also touches `updated_at`, keeping "most recent" ordering
cheap via the `(directory, updated_at)` index. `SetMessages` (the
compressor rewrite) re-persists the whole window transactionally, so
the DB mirrors exactly what the model sees — compressed stubs
included. The `MaxSessionMessages` trim triggers the same resync.

**Save on exit.** `Session.Sync()` is called from a defer in `main`
before the store is closed. It flushes the session's in-memory
metadata to the SQLite row, ensuring that runtime config changes
(`/model`, `/agent`, `/platform`) that did not trigger a turn are
still persisted on exit. The defer ordering guarantees the flush
runs before `Store.Close()`, so the WAL checkpoint captures it. A
`sessionMetaUpdater` callback on the `ConfigOverrider` keeps the
session's metadata in sync with the live config after each runtime
override.

**Redaction.** All persisted content, tool-call args and tool
output run through `internal/redact` at store-write time
(`SecretString` for text, JSON-tree redaction for tool args so the
payload stays parseable). The live in-memory window keeps the raw
text; only disk copies are scrubbed — same policy as audit/memory.

**Resume.** `shmorby -c` resumes the most recent root session for
the launch directory, `-s <id>` a specific one. Sessions are scoped
to their stored working directory: a resume from another cwd chdirs
to it (and says so) *before* config re-load, scope, and project-root
resolution, then config is re-read with the session's stored
provider/model/agent mode applied as flag-level overrides (unless
CLI flags win). The session keeps its id, so `audit session <id>`
stays continuous across restarts. On load, a trailing incomplete
tool-call turn (crash/SIGINT between the assistant `tool_calls` row
and its results) is truncated from both memory and store, so a
restored history is always a valid provider payload.

**Retention.** `session prune` deletes archived/inactive sessions
older than `retention_days` and trims past `max_sessions`, skipping
anything written in the last 5 minutes (a live process touches its
row on every message). The sweep is manual in v1 (run it directly
or from cron); both passes resolve to a single all-or-nothing
delete transaction.

**Scope notes (v1).** Two processes resuming the same session are
last-writer-wins per `(session_id, seq)`. Subagent child sessions
stay in-memory; TUI pickers (`/sessions`), `--fork`, export, and
`/reset`→archive+rotate are planned M2/M3 follow-ups.

---

## 8. Context Compression

Evaluated before every LLM call:

1. Get model's `ContextWindow` (API-fetched at startup; OpenAI falls
   back to a builtin registry of known context windows)
2. Estimate token count (tiktoken, model-resolved — always tokenizer-based)
3. If over `ContextWindow × threshold` (default 80%): compress

**Two-phase compression**:
- Phase 1: Summarize large tool outputs (keep exit code, first/last N
  lines, errors)
- Phase 2: Collapse old message pairs into `[compressed]` summaries
  using an extractive default summarizer that preserves important tail
  content (exit codes, error messages, status markers). When
  `context.summary_model` and/or `context.summary_provider` are set,
  the older half is summarized by the configured LLM instead (empty
  field = use the main provider/model; credentials come from the same
  sections as the main provider). LLM failures and empty results fall
  back to extractive; summary input is capped (head/tail 50k chars)
  and output is bounded by the model's max output tokens.

**Offloading**: before summarization, full messages are saved to SQLite
memory for RAG retrieval. Offloaded messages use a sliding-window summary
(head 250 + tail 250 chars) instead of head-only truncation.

**Memory dedup**: memory context is deduplicated against session content
before injection to avoid overlap with `[compressed]` entries.

**Prompt caching**: messages are ordered with stable prefix first (system
prompt + memory context + tool schemas) for OpenAI automatic prompt
caching. The prefix is byte-identical across all iterations within a turn.

**Modes**: `auto`, `aggressive` (60%), `conservative` (90%), `off`.
`auto` adapts to model size.

**Emergency pass**: when the outgoing request is estimated above 90%
of the context window, one compression pass runs with the mode forced
to `aggressive` for that call only; the configured mode is never
mutated.

**Thread safety**: the same compressor is shared by the main thread
and parallel subagents (`task` tool), so config writes (`/set
context.*`), cached estimates, and counters are mutex- or
atomic-guarded.

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

Ollama embeddings support Matryoshka `dimensions` (64–768) via
`memory.embedding.dimensions`; unset uses the model-native dimension.

**Model info resolution** (cached, API-first):
1. Cache
2. Provider API call (live fetch)
3. OpenAI only: builtin registry of known model context windows (the
   `/v1/models/{model}` API does not return `context_length`)
4. Config override (`models.<name>.context_window`)
5. Fallback: `context.fallback_context_window` (default 128000)

Fallback guesses are cached but re-checked against config overrides on
every lookup, so a later `models.<name>.context_window` override still
applies.

---

## 10. Configuration Layering

Later wins:

1. `/etc/shmorby/config.yaml` (Unix) or `%ProgramData%\shmorby\config.yaml`
   (Windows) — optional
2. `~/.config/shmorby/config.yaml` or `$XDG_CONFIG_HOME/shmorby/config.yaml`
   (Unix) or `%APPDATA%\shmorby\config.yaml` (Windows)
3. `--config` flag
4. `./shmorby.yaml` in cwd
5. CLI flags (`--provider`, `--model`, `--agent`)

Secrets via `api_key` fields in YAML.

Windows path registry (via `internal/xdg/dirs_windows.go`):

| Concept | Unix | Windows |
|---------|------|---------|
| User config | `~/.config/shmorby` | `%APPDATA%\shmorby` |
| System config | `/etc/shmorby` | `%ProgramData%\shmorby` |
| User data | `~/.local/share/shmorby` | `%LOCALAPPDATA%\shmorby` |
| RootPrefix | `/` | `VolumeName(Getwd())\` (e.g. `C:\`) |
| DefaultShell | `$SHELL` or `bash` | `pwsh` → `powershell` → `%ComSpec%` |

- **Workdir:** Unix `~/.local/share/shmorby/workdir`,
  Windows `%LOCALAPPDATA%\shmorby\workdir`
- **Log:** Unix `~/.local/share/shmorby/shmorby.log`,
  Windows `%LOCALAPPDATA%\shmorby\shmorby.log`
- **Audit/Memory DB:** Unix `~/.local/share/shmorby/*.db`,
  Windows `%LOCALAPPDATA%\shmorby\*.db`

---

## 11. Runtime Config Overrides (`/set`)

The `/set <param> <value>` command modifies config at runtime and
propagates changes to live components without restarting. Provider and
API-key switches are also available as `/platform <name>` and
`/apikey` slash commands. `/apikey` always reads the key from a
masked prompt (any inline argument is ignored); prefer it over
`/set apikey <key>`, which keeps the key visible on screen and in
terminal scrollback.

**Overrideable parameters:**

| Param | Type | Example |
|-------|------|---------|
| `provider` | string | `/set provider openai` |
| `model` | string | `/set model gpt-4o` |
| `apikey` | string | `/set apikey sk-proj-...` |
| `agent.default` | string | `/set agent.default diagnose` |
| `agent.max_tool_iterations` | int | `/set agent.max_tool_iterations 30` |
| `agent.shell` | string | `/set agent.shell bash` |
| `tools.timeout` | int | `/set tools.timeout 60` |
| `tools.shell.enabled` | bool | `/set tools.shell.enabled false` |
| `tools.sudo.enabled` | bool | `/set tools.sudo.enabled true` |
| `tools.aws.enabled` | bool | `/set tools.aws.enabled true` |
| `permission.shell` | string | `/set permission.shell deny` |
| `permission.ssh` | string | `/set permission.ssh allow` |
| `permission.sudo` | string | `/set permission.sudo deny` |
| `permission.aws` | string | `/set permission.aws deny` |
| `permission.find` | string | `/set permission.find deny` |
| `permission.file_read` | string | `/set permission.file_read allow` |
| `permission.file_edit` | string | `/set permission.file_edit ask` |
| `permission.file_write` | string | `/set permission.file_write ask` |
| `permission.grep` | string | `/set permission.grep allow` |
| `permission.interactive` | bool | `/set permission.interactive false` |
| `memory.auto_capture` | bool | `/set memory.auto_capture false` |
| `context.mode` | string | `/set context.mode aggressive` |
| `context.enabled` | bool | `/set context.enabled false` |
| `context.threshold` | float | `/set context.threshold 0.9` |
| `context.offload_to_memory` | bool | `/set context.offload_to_memory false` |
| `tui.fullscreen` | bool | `/set tui.fullscreen true` (restart to apply) |
| `tui.theme` | string | `/set tui.theme catppuccin-latte` |
| `tui.glamour.enabled` | bool | `/set tui.glamour.enabled false` |
| `tui.logging.enabled` | bool | `/set tui.logging.enabled false` |
| `tui.logging.default_level` | string | `/set …debug` |

**Propagation**: `ConfigOverrider` (in `internal/agent/setter.go`) writes
the new value into the shared `config.Config` struct and calls component
setters (provider swap, log level, memory toggle, etc.) so changes take
effect immediately. The updated state is reflected in the `/help` overlay's
CONFIG PARAMETERS section.

**Restart-required**: provider API keys/base URLs (`openai.api_key`,
`ollama.base_url`, ...), `scope.*`, `memory.db_path`/`max_entries`/
`embedding.*`, `context.summary_*`/`fallback_context_window`/
`max_tool_output_*`/`min_messages_to_compress`, `models.*`,
`permission.presets`/`permission.rules`, `tui.nav.*`, and
`tui.fullscreen` (alt-screen is fixed at program start) only take
effect on restart. Provider/model/apikey changes recreate the LLM
provider on the fly; theme/logging/memory toggles apply instantly.

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
- Paged `/help` overlay — PgUp/PgDn page, Up/Down scroll, Home/End jump
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

Ops-oriented core, with file tools available in `code` mode:

| Tool | Args | Notes |
|------|------|-------|
| shell | command, cwd, timeout | 120s, trunc >64KiB, 100MiB hard stream cap |
| ssh | host, user, command | Key-based, `BatchMode=yes` |
| sudo | command | Requires `sudo -n`, gated by config |
| aws | args array | Respects AWS env, gated by config |
| find | path, pattern, type, max_depth | Glob search, no shell find; anchored to project root |
| task | tasks[], parallel | Subagent dispatch; own session + inherited perms |
| websearch | query, max_results | SearXNG or Exa backend |
| webfetch | url, max_bytes | HTTP fetch; SSRF guard; max 64 KiB |
| mcp tools | server-defined | `<server>_<tool>` from `mcp.servers` |
| file_read | path, offset, limit | Line-numbered reads; anchored to project root (code mode) |
| file_edit | path, old, new | Single-occurrence string replacement; anchored to project root (code mode) |
| file_write | path, content | Create/overwrite with parent-dir creation; anchored to project root (code mode) |
| grep | pattern, path, include | Regex content search; anchored to project root; 500-result cap (code mode) |

### Subagents (`task` tool)

The `task` tool dispatches independent subtasks to subagents, optionally
in parallel (`parallel: true`, up to 5 concurrent). Each subtask gets
its own agent session with tool access filtered by the parent session's
permission constraints (`FilterByPerm`) and the same provider/model.
Results are returned as a JSON array of per-task outputs. Gated by
`permission.task` (default `ask`); subagent runs are recorded in the
audit DB and surfaced as child tabs in the TUI.

**Timeout protection**: subtask dispatch enforces timeout
guards to prevent indefinite hangs:
- **Per-subtask timeout** (`tools.subtask_timeout`): caps how long
  each subtask can run before context cancellation. When set to 0
  (default), derived from `tools.timeout × max_tool_iterations × 2`.
- **Total timeout** (`TaskOrchestrator.TotalTimeout`): hard upper
  bound on the entire `wg.Wait()` duration. Takes precedence over
  per-subtask timeout.
- Subagents never receive the parent's permission callback (stdin
  reads from parallel goroutines would deadlock the terminal).

### MCP servers

Shmorby connects to Model Context Protocol servers configured under
`mcp.servers`, performs the MCP handshake, and registers each discovered
tool as `<server>_<tool>`. Each server is either a subprocess
(`command`, `args`, `env`) or a remote streamable HTTP endpoint
(`url`, `headers`); exactly one of `command` or `url` must be set.
Header values support `${VAR}` env substitution so secrets stay out of
the config file and are never logged. Gated by `permission.mcp` (default
`ask`). Servers that send `tools/list_changed` notifications are
re-listed automatically. Shut down cleanly on exit.

### Websearch and Webfetch

Both tools are **disabled by default** and available in all agent
modes when enabled.

**websearch** dispatches to one of two backends:

| Backend | Auth | Notes |
|---------|------|-------|
| SearXNG (default) | None (local) | Requires JSON output format enabled |
| Exa | `exa_api_key` | Cloud API, no local setup |

**SearXNG requirements**: The instance must have JSON format enabled
in `settings.yml`:

```yaml
search:
  formats:
    - html
    - json
```

The tool appends `&format=json` to the query URL. Without JSON
enabled, SearXNG returns HTML and parsing fails.

**webfetch** enforces SSRF protection:
- Blocks private/loopback IPs (127.0.0.1, 10.x, 172.16-31.x, 192.168.x)
- Resolves hostnames and checks all IPs at connection time via
  custom `DialContext` (closes TOCTOU gap from DNS rebinding)
- CheckRedirect validates IPs on every redirect hop
- Default max: 64 KiB, absolute max: 1 MiB
- Configurable timeout (default 120s via `tools.timeout`),
  strips null bytes from response

**Command timeout ceiling** (shell, ssh, sudo, aws): per-call
`timeout_seconds` is clamped to 3600s (1 hour) to prevent resource
exhaustion attacks.

**Web search flow**: Operate and diagnose modes include `websearch`
and `webfetch` alongside shell/ssh/sudo/aws tools. Chat mode filters
to only `websearch` and `webfetch`. When the model calls `websearch`,
the tool dispatches to the configured backend (SearXNG or Exa), parses
the JSON response, and returns formatted results (title, snippet, URL).
The `webfetch` tool fetches a page body directly. Both use Go's
`net/http` with a configurable timeout.

---

## 14. Environment Ledger

The encrypted environment ledger (`internal/ledger`) persists
cross-session knowledge: known hosts, roles, credential locations,
past incidents, open tickets. Every session starts with this context
instead of discovering from scratch.

**Storage layout** (paths via `internal/xdg`):
- Unix: `~/.local/share/shmorby/ledger.json.age` — encrypted JSON
- Unix: `~/.local/share/shmorby/ledger.key` — age x25519 identity
  (0600)
- Unix: `~/.local/share/shmorby/ledger.lock` — flock for concurrency
- Windows: `%LOCALAPPDATA%\shmorby\ledger.json.age` — encrypted JSON
- Windows: `%LOCALAPPDATA%\shmorby\ledger.key` — age x25519 identity
  (0600 best-effort, inherited ACL)
- Windows: `%LOCALAPPDATA%\shmorby\ledger.lock` — LockFileEx for
  concurrency

**Data model**: section-keyed store (`map[string]json.RawMessage`)
so output stays jq-friendly. Well-known sections include `hosts`,
`incidents`, `tickets`, but any section name is accepted.

**Concurrency**: exclusive `flock` (Unix) / `LockFileEx` (Windows)
on a separate lock file serialises read-modify-write across
processes. The lock is held for the duration of the CLI command.

**Size limits**: writes are capped at 64 KiB per section and 100
sections. Reads enforce matching bounds — an encrypted file over
12 MiB or a decrypted payload over 10 MiB is rejected instead of
loaded into memory.

**Atomic write**: `os.WriteFile` to `ledger.json.age.tmp` then
`os.Rename` (same pattern as `config/migrate.go`; Windows retries
with `Remove`+`Rename` on sharing violation).

**Encryption**: age x25519 (filippo.io/age). The key is generated
on first use and stored with chmod 0600. Data is never written in
plaintext.

**CLI**: `shmorby ledger list|get|set|delete <section>`. The CLI
enforces the same size caps and secret redaction as the agent tool.

**Agent tools** (when `ledger.enabled: true`, the default):
- `ledger_get` — reads a section; read-only, available in diagnose
  mode for consulting known-good state during troubleshooting
- `ledger_set` — writes a section; operate-level permission (mutating,
  subject to rules/"ask"); payloads are redacted via `internal/redact`
  before storage to prevent secret leakage

**Redaction**: tool output is redacted via `redact.SecretString()` in
`processToolCall()` before being returned to the LLM context. This
prevents the LLM provider (OpenAI, OpenRouter, OpenCode Zen) from
receiving credentials captured in tool output (e.g., `env`,
`cat /etc/shadow`, `kubectl get secret`). The raw output is still
emitted to the UI for user visibility. Audit logs and memory stores
apply the same redaction independently via `internal/redact`. The
redaction engine walks the decoded JSON tree (`redact.JSONData`):
values under secret-named keys (`password`, `api_key`, `secret`,
`token`, ...) are replaced with `[REDACTED]`, remaining string values
are pattern-matched (AKIA keys, Bearer tokens, ...), and numeric
precision is preserved. Unlike regex-on-text redaction, stored data
always remains valid JSON. Both the agent tool and the CLI apply the
same redaction, and `ledger_set` writes are audited (section +
redacted payload).

**Context injection**: at session start, the ledger is opened and a
compact "Known environment (ledger):" block is formatted and injected
into the system prompt (after memory context), budgeted by
`ledger.context_budget` (default 2048 bytes; `0` = unlimited). This
gives new sessions immediate access to discovered facts without
re-discovery. `ledger.enabled` is read at startup: toggling it via
`/set` requires a restart (tools are registered and context is
precomputed when the session starts).

**Size caps**: max 64 KB JSON per section, max 100 sections. Oversized
writes fail with a clear error instead of corrupting the ledger.

**Concurrency**: agent tools open the ledger per call (lock held only
for the duration of the tool call), so concurrent `shmorby ledger get`
while the agent runs does not block beyond one call.

---

## 14b. Self-Diagnostics (`shmorby doctor` + structured health)

Degraded tooling is detected and surfaced instead of producing a
confusing downstream failure.

**Package `internal/health`**:

- **Preflights**: `sudo -n true`, ssh reachability (`LookPath` +
  `ssh -V`), provider API reachability (ollama/openai/openrouter
  base URLs), web fetch (`https://example.com`), local exec sanity
  (`echo ok`), and aws CLI presence. `shmorby doctor` runs all on
  demand and prints a table (`CHECK  STATUS  DURATION  DETAILS`);
  non-zero exit when any check is degraded/missing.
- **Normalized failure**: `Degraded{Tool string; Reason string; ` +
  `Duration time.Duration}` wraps external-call sites: `OSExecutor.Run`
  (`internal/exec/exec.go`), `SSHTool.Run`, `AWSTool`, `SudoTool`,
  and web tools. `context.DeadlineExceeded` → `"timeout"`,
  auth failures → `"auth/creds"`, missing binary → `"missing"`,
  `unreachable` for dial/host errors, else `"error"`. Implements
  `Unwrap` so `errors.Is/As` still works.
- **Surfacing**: the agent loop (`internal/agent`) collects
  `*health.Degraded` during a turn and prefixes the final assistant
  text with `> ⚠ degraded tooling detected — run ` + "`shmorby doctor`" +
  ` for details` plus per-tool reasons. Thin wrapper; no behavior
  change to successful paths.

---

## 15. Key Design Decisions

- **Go over Python/Node**: performance for shell execution, single
  binary, stdlib-first
- **Shell over file tools (default)**: models a sysadmin's actual
  workflow; `code` mode adds file tools as an opt-in extension
- **Provider-agnostic via interface**: swap LLMs without agent logic
  changes
- **Project root anchoring (code mode)**: launch CWD becomes the
  project root for file tools; paths outside the root are blocked unless
  allowlisted via `code.allowed_patterns`. Blocked patterns (.git/, etc.)
  are always rejected. `file_read` rejects non-regular/oversized files;
  `file_edit` writes atomically. Operate/diagnose modes remain rootless
  (run from anywhere).
- **API-fetched model info**: context windows adapt to the actual model,
  no hardcoding (exception: OpenAI ships a builtin registry of known
  context windows because its API omits `context_length`; Ollama
  similarly omits `context_length` for most models)
- **Local-first memory**: SQLite + chromem-go, no external services, no
  data leaks
- **Glob-based permission rules**: familiar pattern, expressive without
  regex complexity


