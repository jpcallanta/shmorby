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
| Linux/macOS/Windows | Tested on Linux; macOS and Windows support in progress |
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

1. `/etc/shmorby/config.yaml` (Unix) / `%ProgramData%\shmorby\config.yaml` (Windows) — skipped if missing
2. `~/.config/shmorby/config.yaml` or `$XDG_CONFIG_HOME/shmorby/config.yaml` (Unix) / `%APPDATA%\shmorby\config.yaml` (Windows)
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
--agent string          Agent mode: operate, diagnose (default "operate")
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
| `context` | Token estimation and compression (heuristic/tiktoken), threshold-based, offload-to-memory |
| `models` | Per-model context window and max output token overrides |
| `tui` | Theme, glamour markdown rendering, logging panel, navigation keybinds |

## Slash commands

| Command | Description |
|---------|-------------|
| `/help` | Show help overlay with keybindings, commands, modes |
| `/set <param> <value>` | Override a config parameter at runtime |
| `/quit` | Exit shmorby |
| `/reset` | Clear conversation history |
| `/model <name>` | Switch LLM model |
| `/agent <mode>` | Switch agent mode (operate, diagnose) |
| `/scope` | Show loaded scope context and size |
| `/memory` | Memory management (search, forget, clear, stats) |
| `/context` | Token usage and compression stats |
| `/log <level>` | Set log verbosity (debug, info, warn, error) |
| `/tui` | Toggle fullscreen mode |

## Agent modes

### Operate (default)

Full shell, SSH, sudo, and AWS tool access. Follows the
observe → plan → execute → verify cycle.

### Diagnose

Read-only inspection. Shell is restricted by a mutating-command guard that
blocks `rm`, `mv`, `dd`, `mkfs`, package install/remove, systemctl
start/stop, and redirects to `/etc`.

Switch with `Tab`/`Shift+Tab` (empty input), `/agent diagnose` and
`/agent operate` in the TUI, or set `agent.default` in config.

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

## Permission system

Permissions gate shell, SSH, sudo, and AWS commands:

| Permission | Default | Effect |
|------------|---------|--------|
| `shell` | ask | Requires approval |
| `ssh` | ask | Requires approval |
| `sudo` | ask | Requires approval; tool disabled by default (`tools.sudo.enabled: false`) |
| `aws` | ask | Requires approval; tool disabled by default (`tools.aws.enabled: false`) |

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

