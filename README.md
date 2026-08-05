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
| Go 1.24+ | For building from source |
| Linux/macOS | Supported platforms (x86_64, arm64) |
| LLM provider | One of: Ollama (local, free), OpenAI, OpenRouter, OpenCode Zen |

## Quick start

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
model: gpt-4o
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
| Models | `gpt-4o`, `gpt-4o-mini`, `o1`, `o3-mini` |
| Azure | Set `openai.base_url` to your Azure endpoint |

```yaml
provider: openai
model: gpt-4o
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

1. `/etc/shmorby/config.yaml` — skipped if missing
2. `~/.config/shmorby/config.yaml` or `$XDG_CONFIG_HOME/shmorby/config.yaml`
3. `--config` flag (error if set but missing)
4. `./shmorby.yaml` in current directory
5. CLI flags (`--provider`, `--model`, `--agent` — always win)

See [`examples/shmorby.yaml`](examples/shmorby.yaml) for the full reference.

### CLI flags

```
shmorby [flags]

--validate              Validate config and exit
--provider string       LLM provider: openrouter, opencode_zen, openai, ollama (default "ollama")
--model string          Model name (default "llama3.2")
--config string         Config file path
--scope-file string     Operational context markdown (SCOPE.md)
--agent string          Agent mode: operate, diagnose, chat (default "operate")
--system-prompt-file    Override system prompt file
--no-tui                Disable TUI, use plain stdin/stdout REPL
--log-level string      Log level: debug, info, warn, error (default "info")
--version               Print version and exit
```

### Additional config sections

See [`examples/shmorby.yaml`](examples/shmorby.yaml) for:

| Section | Purpose |
|---------|---------|
| `memory` | SQLite-backed memory with vector search, embedding (ollama/openai), auto-capture, tag rules |
| `context` | Token estimation (tiktoken, model-resolved) and compression, threshold-based, offload-to-memory |
| `models` | Per-model context window and max output token overrides |
| `tui` | Theme, glamour markdown rendering, logging panel, navigation keybinds |
| `websearch` | Web search via SearXNG or Exa API backend (all modes) |
| `webfetch` | URL fetching for page content (all modes) |

### Web search and fetch configuration

Both tools are **disabled by default** and available in all agent modes when enabled.

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
| `/agent <mode>` | Switch agent mode (operate, diagnose, chat) |
| `/scope` | Show loaded scope context and size |
| `/memory` | Memory management (search, forget, clear, stats) |
| `/context` | Token usage and compression stats |
| `/log <level>` | Set log verbosity (debug, info, warn, error) |
| `/tui` | Toggle fullscreen mode |

## Agent modes

### Operate (default)

Full shell, SSH, sudo, and AWS tool access. Also includes `websearch` and
`webfetch` when enabled. Follows the observe → plan → execute → verify cycle.

### Diagnose

Read-only inspection with `websearch` and `webfetch` available when enabled.
Shell is restricted by a mutating-command guard that blocks `rm`, `mv`, `dd`,
`mkfs`, package install/remove, systemctl start/stop, and redirects to `/etc`.

### Chat

General conversation and research. Advertises `websearch` and `webfetch`
tools — no infrastructure tooling. Both tools are disabled by default;
enable them in `tools.websearch.enabled` and `tools.webfetch.enabled` in
config. See `examples/shmorby.yaml` for configuration.

Switch with `Tab`/`Shift+Tab` (empty input), `/agent diagnose`, `/agent chat`,
and `/agent operate` in the TUI, or set `agent.default` in config.

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

Permissions gate shell, SSH, sudo, and AWS commands:

| Permission | Default | Effect |
|------------|---------|--------|
| `shell` | ask | Requires approval |
| `ssh` | ask | Requires approval |
| `sudo` | ask | Requires approval; tool disabled by default (`tools.sudo.enabled: false`) |
| `aws` | ask | Requires approval; tool disabled by default (`tools.aws.enabled: false`) |
| `websearch` | ask | Follows `shell` permission level; tool disabled by default (`tools.websearch.enabled: false`) |
| `webfetch` | ask | Follows `shell` permission level; tool disabled by default (`tools.webfetch.enabled: false`) |

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
  be configured.
- **ssh** — uses `BatchMode=yes` and `StrictHostKeyChecking=accept-new`.
  Requires SSH key-based auth.
- **aws** — uses the AWS CLI. Credentials from env/credentials file.

