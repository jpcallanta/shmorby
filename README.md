# Shmorby

Shmorby is an AI sysadmin agent that operates infrastructure via shell,
SSH, sudo, and AWS CLI commands. It handles deployment, configuration,
monitoring, and diagnostics — like a senior SRE you can talk to.

> **⚠️ Read [WARNING.md](WARNING.md) before use.** Shmorby can execute arbitrary
> commands on your systems. The author(s) are not liable for damage, data loss,
> or security breaches.

## Requirements

| Requirement | Notes |
|-------------|-------|
| Go 1.25+ | For building from source |
| Linux/macOS | Supported (x86_64, arm64) |
| Windows 10/11 | Experimental (amd64, arm64); Windows Terminal recommended |
| LLM provider | One of: Ollama (local, free), OpenAI, OpenRouter, OpenCode Zen |

## Quick start

### Bootstrap installer

```bash
# Linux / macOS
git clone https://github.com/pwnderpants/shmorby
cd shmorby
./install.sh  # deps, build, install to ~/.local/bin, pull embedding
shmorby
```

```powershell
# Windows (PowerShell 5.1+)
git clone https://github.com/pwnderpants/shmorby
cd shmorby
Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass
.\install.ps1  # deps, build to %LOCALAPPDATA%\shmorby, pull embedding
.\shmorby.exe
# Requires mingw for CGO: choco install mingw
```

Or build from source and run directly (below).

### With Ollama (local, free)

```bash
# 1. Install and start Ollama
curl -fsSL https://ollama.com/install.sh | sh
ollama pull llama3.2
ollama serve &

# 2. Build and run shmorby
git clone https://github.com/pwnderpants/shmorby
cd shmorby
go build -o shmorby ./cmd/shmorby
./shmorby

# 3. Type a task
# ❯ check disk usage on this host
```

> **Note:** Quantized local models (e.g. `llama3.2`) may lack the
> accuracy and reasoning ability needed for reliable sysadmin tasks.
> For best results, use a flagship LLM via OpenAI or Opencode Zen.

### With OpenAI

```yaml
# shmorby.yaml
provider: openai
model: gpt-5.6-sol
openai:
  api_key: sk-proj-...
```

```bash
go build -o shmorby ./cmd/shmorby
./shmorby
```

### With OpenRouter

```yaml
# shmorby.yaml
provider: openrouter
model: openai/gpt-4o
openrouter:
  api_key: sk-or-...
```

```bash
go build -o shmorby ./cmd/shmorby
./shmorby
```

## Provider setup

### Ollama

| | |
|-|-|
| Config value | `ollama` |
| API key | _none_ (runs locally) |
| Default URL | `http://127.0.0.1:11434` |
| Default model | `llama3.2` |
| Note | Ollama must be running (`ollama serve`) |

### OpenAI

| | |
|-|-|
| Config value | `openai` |
| API key | `openai.api_key` in YAML |
| Recommended | `gpt-5.6-sol`/`terra`/`luna` |
| Other | `gpt-5.4`, `gpt-5.4-mini`, `gpt-4.1`, `o3`, `o4-mini` |
| Azure | Set `openai.base_url` to your Azure endpoint |

```yaml
provider: openai
model: gpt-5.6-sol
openai:
  api_key: sk-proj-...
  timeout: 120
```

### OpenRouter

| | |
|-|-|
| Config value | `openrouter` |
| API key | `openrouter.api_key` in YAML |
| Models | Any [OpenRouter model](https://openrouter.ai/models) |

### OpenCode Zen

| | |
|-|-|
| Config value | `opencode_zen` |
| API key | `opencode_zen.api_key` in YAML |
| Default URL | `https://opencode.ai/zen` |

## Configuration

Shmorby loads config with layered precedence (later wins):

1. `/etc/shmorby/config.yaml` (Unix) or `%ProgramData%\shmorby\config.yaml`
   (Windows) — skipped if missing
2. `~/.config/shmorby/config.yaml` or `$XDG_CONFIG_HOME/shmorby/config.yaml`
   (Unix) or `%APPDATA%\shmorby\config.yaml` (Windows) — skipped if missing
3. `--config` flag (error if set but missing)
4. `./shmorby.yaml` in current directory
5. CLI flags (`--provider`, `--model`, `--agent` — always win)

See [`examples/shmorby.yaml`](examples/shmorby.yaml) (Unix) and
[`examples/shmorby.windows.yaml`](examples/shmorby.windows.yaml)
(Windows) for the full reference.

Windows paths (via `internal/xdg/dirs_windows.go`):

| Concept | Path |
|---------|------|
| User config | `%APPDATA%\shmorby\config.yaml` |
| System config | `%ProgramData%\shmorby\config.yaml` |
| User data | `%LOCALAPPDATA%\shmorby\` |
| Workdir | `%LOCALAPPDATA%\shmorby\workdir` |
| Vectors | `%LOCALAPPDATA%\shmorby\vectors\` |
| Ledger | `%LOCALAPPDATA%\shmorby\ledger.json.age` / `ledger.key` / `ledger.lock` |

User data includes `memory.db`, `audit.db`, `sessions.db`,
`shmorby.log`, `ledger.json.age`, `ledger.key`, `ledger.lock`.

### CLI flags

```
shmorby [flags]

--validate              Validate config and exit
--provider string       LLM provider: openrouter, opencode_zen, openai, ollama (default "ollama")
--model string          Model name (default "llama3.2")
--config string         Config file path
--scope-file string     Operational context markdown (SCOPE.md)
--agent string          Agent mode: operate, diagnose, chat, code (default "operate")
--system-prompt-file    Override system prompt file
--no-tui                Disable TUI, use plain stdin/stdout REPL
--log-level string      Log level: debug, info, warn, error (default "info")
-c, --continue          Resume the most recent session for this directory
-s, --session string    Resume a specific session by id
--version               Print version and exit

Subcommands:
  config migrate          Merge missing config fields from defaults
                          (exhaustive: --dry-run lists every absent
                          enabled=false and non-empty default)
  config show             Print default config as YAML (matches migrate)
  config validate         Validate a config file
  audit list              List audit entries with optional filters
  audit get <id>          Show a single audit entry with output
  audit session <id>      Show all audit entries for a session
  audit export            Export audit entries (json or csv)
  audit vacuum            Archive and remove old audit entries
  audit stats             Show audit DB statistics
  session list            List persisted sessions (current directory)
  session show <id>       Show session metadata or full transcript
  session rm <id>         Archive a session (--force deletes rows)
  session prune           Apply session retention policy
  ledger list             List encrypted environment ledger sections
  ledger get <section>    Print a ledger section as JSON
  ledger set <section>    Replace a ledger section with JSON
  ledger delete <section> Remove a ledger section
  doctor                  Run self-diagnostics and report tool health
```

### Self-diagnostics (`shmorby doctor`)

Preflight checks for external and internal dependencies; `shmorby doctor`
runs all on demand and prints a table:

```
CHECK          STATUS   DURATION  DETAILS
------------------------------------------------------------
local-exec     ok            4ms  exec echo ok
sudo           degraded     12ms  sudo: a password is required
ssh            ok            2ms  ssh client present
provider       ok           18ms  ollama http://127.0.0.1:11434 -> 200
webfetch       ok           45ms  GET https://example.com/ -> 200
aws            missing       1ms  aws CLI not found in PATH
memory-sqlite  ok           12ms  /home/user/.local/share/shmorby/memory.db (2.1MB)
memory-ollama  ok           25ms  /api/embed -> 200 (768-dim)
ledger          ok            3ms  /home/user/.local/share/shmorby/ledger.json.age (4 entries)
audit-sqlite   ok            8ms  /home/user/.local/share/shmorby/audit.db (156 entries)
config          ok            1ms  /home/user/.config/shmorby/config.yaml
xdg             ok            0ms  data/config all writable
------------------------------------------------------------
```

**External checks** (original): local exec, sudo, ssh client, provider
API reachability, web fetch, and aws CLI.

**Internal checks** (new):
- `memory-sqlite` — SQLite memory DB integrity (`PRAGMA integrity_check`),
  read/write probe, file size. Skipped (ok) when no path configured or DB
  not yet created.
- `memory-ollama` — Ollama `/api/embed` endpoint probe (independent of the
  generic `/api/tags` provider check). Only runs when `provider == "ollama"`;
  skipped otherwise.
- `ledger` — Encrypted ledger file presence, permissions (must be `0600`),
  decryptability, and entry count. No secrets are exposed in output.
- `audit-sqlite` — SQLite audit DB integrity, entry count, file size.
  Skipped (ok) when no path configured or DB not yet created.
- `config` — Config file parses as valid YAML, required fields present
  (`provider`, `model`), warns on unknown provider values.
- `xdg` — `$XDG_DATA_HOME` and `$XDG_CONFIG_HOME` (with `~/.local/share`
  and `~/.config` fallbacks) all exist and are writable.

Degraded tooling is also surfaced as a structured diagnostic at the top
of agent responses (`> ⚠ degraded tooling detected — run ` + "`shmorby doctor`" + ` for details`) via `internal/health.Degraded{Tool, Reason, Duration}` wrapping `OSExecutor.Run`, `SSHTool.Run`, `AWSTool`, and web tools. `context.DeadlineExceeded` → `timeout`, auth failures → `auth/creds`, etc. Thin wrapper; no behavior change to successful paths.

### Config migrate (exhaustive backfill)

`shmorby config migrate --dry-run --file <path>` reports every absent
default as `+ <dotted.path>` (sorted, `enabled=false` included) and
exits 0; when up-to-date it prints “No missing fields — config is up
to date.” `migrate --file <path>` preserves comments and permissions
(atomically via `*.tmp` + `os.Rename`, `MkdirAll 0755`) and produces a
`ValidateFile`-clean YAML that round-trips. Empty files are treated as
a blank mapping (not `null`). Unknown user keys are preserved. Type
mismatches (user scalar vs default mapping, e.g. `tools: "oops"`) are
surfaced as `+ <path> (type mismatch: expected mapping)` via
`--dry-run` instead of silent ignore.

Zero-value handling: `enabled=false` is intentionally injected so
`migrate` and `config show` agree; empty `api_key`, `models: {}`,
`permission.presets: []` etc. remain omitted intentionally (see
`internal/config/migrate.go:filterZeroValues` and
`examples/shmorby.yaml`).

### Additional config sections

See [`examples/shmorby.yaml`](examples/shmorby.yaml) for:

| Section | Purpose |
|---------|---------|
| `memory` | SQLite + vectors, embedding, auto-capture, tag rules |
| `context` | Token estimation, compression, offload-to-memory |
| `models` | Per-model context window + max output overrides |
| `tui` | Theme, glamour, logging panel, keybinds |
| `tools.websearch` | Web search via SearXNG/Exa (disabled by default) |
| `tools.webfetch` | URL fetching for page content (disabled by default) |
| `mcp` | MCP servers (subprocess or remote HTTP); tools per server |
| `audit` | SQLite audit store: runs, outputs, perms, subagents |
| `session` | Conversation persistence, resume, retention (see below) |
| `ledger` | Encrypted environment ledger; agent tools + context injection |
| `code` | Project root anchoring for code mode file tools |

### Session persistence

Conversations are saved continuously (write-through, SQLite at
`session.db_path`, default `~/.local/share/shmorby/sessions.db`) so
they survive exits and crashes, and can be resumed. On program exit
(`/quit`, Ctrl+C, SIGTERM), session metadata is flushed to the store
so runtime changes (e.g. `/model`, `/agent`, `/platform`) persist even
without a subsequent turn.

```
shmorby -c                 # resume most recent session for this directory
shmorby -s <id>            # resume a specific session
shmorby session list       # root sessions for the current directory
shmorby session show <id> --messages
shmorby session rm <id>    # archive; --force deletes rows
shmorby session prune      # apply retention_days / max_sessions
```

Sessions are scoped to their working directory: resuming a session
started elsewhere chdirs back to it (and says so) before anything
else starts. The resumed conversation keeps the same session id, so
`shmorby audit session <id>` spans restarts. Only the message history
is restored — system prompt, scope, memory and ledger context are
rebuilt fresh at startup. A session interrupted mid-turn restores to
the last complete turn (dangling tool calls are dropped). With
`session.enabled: false` nothing is written and the resume flags
error cleanly. Retention cleanup (`session prune`) is manual in v1
— run it directly or from cron; sessions written in the last 5
minutes are always skipped as possibly in use.

> **Privacy note:** everything persisted is first run through the
> same secret redaction as `audit`/`memory` (keys, tokens,
> passwords), but sessions still contain redacted-but-otherwise-raw
> operational history: hostnames, paths, and command output.
> `sessions.db` is created `0600`; treat it as sensitive.

### Web search and fetch configuration

Both tools are **disabled by default** and available in all agent
modes when enabled.

#### websearch

```yaml
tools:
  websearch:
    enabled: true
    engine: searxng          # "searxng" (local) or "exa" (cloud)
    base_url: http://localhost:8888  # SearXNG instance URL (required for searxng)
    exa_api_key: ""          # Required for exa engine
```

| Field | Default | Description |
|-------|---------|-------------|
| `enabled` | `false` | Enable the websearch tool |
| `engine` | `searxng` | Search backend: `searxng` (local, no CAPTCHAs) or `exa` (cloud API) |
| `base_url` | — | SearXNG instance URL (required when engine is `searxng`) |
| `exa_api_key` | — | Exa API key (required when engine is `exa`) |

**SearXNG setup**: Your SearXNG instance must have **JSON output format enabled**. In `settings.yml`:

```yaml
search:
  formats:
    - html
    - json    # Required for shmorby
```

Default SearXNG URL: `http://localhost:8888` (typical Docker/port config).

#### webfetch

```yaml
tools:
  webfetch:
    enabled: true
```

| Field | Default | Description |
|-------|---------|-------------|
| `enabled` | `false` | Enable the webfetch tool |

**Behavior**:
- Fetches any HTTP/HTTPS URL and returns plain text
- **SSRF protection**: blocks private/loopback IPs (127.0.0.1, 10.x, 192.168.x, etc.)
- Default max: 64 KiB; absolute max: 1 MiB
- Configurable timeout per request (default 120s, set via `tools.timeout`)
- Strips null bytes from response

## Slash commands

| Command | Description |
|---------|-------------|
| `/help` | Show help overlay with keybindings, commands, modes |
| `/set <param> <value>` | Override a config parameter at runtime |
| `/quit` | Exit shmorby |
| `/reset` | Clear conversation history |
| `/model <name>` | Switch LLM model |
| `/platform <name>` | Switch LLM provider |
| `/apikey` | Set API key for current provider (input masked) |
| `/agent <mode>` | Switch agent mode (operate, diagnose, chat, code) |
| `/scope` | Show loaded scope context and size |
| `/memory` | Memory management (search, forget, clear, stats) |
| `/context` | Token usage and compression stats |
| `/log <level>` | Set log verbosity (debug, info, warn, error) |
| `/tui` | Toggle fullscreen mode |

## Agent modes

### Operate (default)

Full shell, SSH, sudo, and AWS tool access. Also includes the `find`
tool (glob-based file search), the `task` tool (parallel subagent
dispatch), any MCP server tools when configured, plus `websearch` and
`webfetch` when enabled. Follows the observe → plan → execute → verify
cycle.

### Diagnose

Read-only inspection with `websearch` and `webfetch` available when enabled.
Shell is restricted by a mutating-command guard that blocks `rm`, `mv`, `dd`,
`mkfs`, package install/remove, systemctl start/stop, redirects to `/etc`,
`chmod`/`chown` on sensitive paths, `eval`, command substitution and
`curl`/`wget` piped to a shell.

### Chat

General conversation and research. Advertises `websearch` and `webfetch`
tools — no infrastructure tooling. Both tools are disabled by default;
enable them in `tools.websearch.enabled` and `tools.webfetch.enabled` in
config. See `examples/shmorby.yaml` for configuration.

### Code

Coding agent mode for reading, editing, writing, and reasoning about
codebases. Advertises `file_read`, `file_edit`, `file_write`, `find`,
`grep`, `shell`, and `task` tools. Useful for software development tasks
like code review, refactoring, bug fixes, and feature implementation.
The agent follows a read-before-write workflow and produces minimal diffs.

**Project root anchoring** (like opencode): the directory shmorby is
launched from becomes the project root. File tools are confined to the
root — paths that resolve outside it are rejected, and symlinks are
evaluated so `../` + symlink escapes cannot bypass the boundary. To
operate on a specific path outside the root, add a glob to
`code.allowed_patterns`. Blocked patterns (`.git/`, `vendor/`,
`node_modules/`, `.idea/`, `.vscode/`) are always rejected. Override the
default root with `code.workdir` in config. See the `code` config section
in `examples/shmorby.yaml`.

Switch with `Tab`/`Shift+Tab` (empty input), `/agent diagnose`, `/agent chat`,
`/agent code`, and `/agent operate` in the TUI, or set `agent.default` in config.

## Examples

```text
❯ deploy nginx reverse proxy for app on :8080
✓ Installed nginx
✓ Created /etc/nginx/sites-available/app
✓ Enabled site and reloaded nginx
✓ curl http://localhost:8080 → 200 OK
```

```text
❯ check disk usage on all production hosts
$ df -h | awk '$5 > 80%'
server1: /dev/sda1  92%  (14G available)
server3: /dev/sdb1  85%  (28G available)
```

```text
❯ why is the api pod in crashloop
$ kubectl describe pod api-7f8b9c --tail=20
$ kubectl logs api-7f8b9c --previous
Exit code 1: database connection refused at /app/db.go:42
```

```text
❯ /agent chat
Switched to chat mode.
❯ what's the latest on the ozone layer?
[websearch: "ozone layer 2026 news"]
According to NOAA, the 2026 Antarctic ozone hole...
```

## Permission system

Permissions gate shell, SSH, sudo, AWS, subagent, MCP, and file
tools:

| Permission | Default | Effect |
|------------|---------|--------|
| `shell` | ask | Requires approval |
| `ssh` | ask | Requires approval |
| `sudo` | ask | Requires approval; disabled by default |
| `aws` | ask | Requires approval; disabled by default |
| `task` | ask | Gates subagent dispatch (operate mode) |
| `mcp` | ask | Gates MCP tools (when `mcp.servers` set) |
| `websearch` | ask | Follows `shell` perm; disabled by default |
| `webfetch` | ask | Follows `shell` perm; disabled by default |
| `find` | allow | Gates glob file search tool |
| `file_read` | allow | Gates file-read tool (read-only) |
| `file_edit` | ask | Gates file-edit tool |
| `file_write` | ask | Gates file-write tool |
| `grep` | allow | Gates grep tool |

Set in the `permission` section of config. Options: `allow`, `ask`, `deny`.

### Interactive prompts

When `permission.interactive` is `true` (default `true`), tools with `ask`
level show an inline y/n/a prompt:

- **y** — allow this tool call
- **n** — deny this tool call
- **a** — allow all subsequent calls for this tool in the current message

Prompts appear in the TUI (inline) or REPL (stdin). When `interactive` is
`false`, `ask` tools default to allow (v1 backward-compatible behavior).

### Permission presets

Built-in command-level presets (see `internal/tools/presets.go`):

| Preset | Purpose |
|--------|---------|
| `destructive` | Blocks `rm -rf`, `mkfs`, `dd`, `shred` |
| `service` | Gates `systemctl stop/restart/disable` |
| `package` | Allows install, gates remove |
| `network` | Gates `iptables`, `ufw`, `netplan` |
| `user` | Gates user/group add/mod, blocks deletion |
| `ssh` | Allows `ssh`, `scp`, `rsync` |
| `aws` | Allows describe/ls, gates S3 delete, blocks instance termination |
| `sudo` | Gates `sudo` service/user commands, blocks user deletion |

Custom presets can be added in `permission.presets` and override built-ins.
Custom rules (`permission.rules`) take precedence over presets.

### Custom rules

```yaml
permission:
  rules:
    - match: "rm -rf /"
      action: deny
      reason: "destruction of root filesystem"
    - match: "aws ec2 terminate-instances *"
      action: deny
      reason: "instance termination"
```

Rules use glob matching. The first matching rule wins. Tool-level `deny`
always wins regardless of rules.

### Blast radius

- **sudo** — requires `sudo -n` (non-interactive). Passwordless sudo must
  be configured. On Windows `sudo` is disabled by default (`tools.sudo`
  `enabled: false`); use `gsudo` manually if needed.
- **ssh** — uses `BatchMode=yes` and `StrictHostKeyChecking=accept-new`.
  Requires SSH key-based auth. OpenSSH ships on Windows 10 1809+;
  smoke-tested via `install.ps1`.
- **aws** — uses the AWS CLI. Credentials from env/credentials file.

## Building on Windows

```powershell
# Native build (requires mingw for CGO + go-sqlite3)
choco install mingw        # or: scoop install mingw
go env -w CGO_ENABLED=1
go build -o shmorby.exe ./cmd/shmorby
.\shmorby.exe --validate
.\shmorby.exe --validate --config examples\shmorby.windows.yaml

# Via Makefile
make build-windows          # amd64
make build-windows-arm64    # arm64
```

Planned release artifact (when workflow added):
`shmorby-windows-amd64.zip` (contains `shmorby.exe` +
`examples\shmorby.windows.yaml`). Code signing is
optional. Cross-compile from Linux requires `zig cc`
or `mingw`; native Windows build is the v1 recommendation.

Use `--no-tui` as primary on Windows if `getTermSize`
fails; TUI (bubbletea) works in Windows Terminal but
falls back to REPL automatically.

On Windows the shell dispatch in `internal/tools/shell.go`
uses `powershell -NoProfile -NonInteractive -Command`,
`cmd /d /c`, or `bash -c` derived from
`agent.shell`. Empty `agent.shell` prefers `pwsh` over
`powershell` when in `PATH`, else `%ComSpec%`.

