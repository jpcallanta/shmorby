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
| Linux/macOS | Tested on Linux; macOS support in progress |
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

```bash
export OPENAI_API_KEY=sk-...
go build -o shmorby ./cmd/shmorby
./shmorby --provider openai --model gpt-4o
```

### With OpenRouter

```bash
export OPENROUTER_API_KEY=...
go build -o shmorby ./cmd/shmorby
./shmorby --provider openrouter --model openai/gpt-4o
```

## Provider setup

### Ollama

| | |
|-|-|
| Config value | `ollama` |
| Env vars | _none_ (runs locally) |
| Default URL | `http://127.0.0.1:11434` |
| Default model | `llama3.2` |
| Note | Ollama must be running (`ollama serve`) |

### OpenAI

| | |
|-|-|
| Config value | `openai` |
| Required env | `OPENAI_API_KEY` |
| Models | `gpt-4o`, `gpt-4o-mini`, `o1`, `o3-mini` |
| Azure | Set `openai.base_url` to your Azure endpoint |

```yaml
provider: openai
model: gpt-4o
openai:
  api_key_env: OPENAI_API_KEY
  timeout: 120
```

### OpenRouter

| | |
|-|-|
| Config value | `openrouter` |
| Required env | `OPENROUTER_API_KEY` |
| Models | Any [OpenRouter model](https://openrouter.ai/models) |

### OpenCode Zen

| | |
|-|-|
| Config value | `opencode_zen` |
| Required env | `OPENCODE_ZEN_API_KEY` |
| Default URL | `https://opencode.ai/zen` |

## Configuration

Shmorby loads config with layered precedence (later wins):

1. `/etc/shmorby/config.yaml` (skipped if missing)
2. `~/.config/shmorby/config.yaml` or `$XDG_CONFIG_HOME/shmorby/config.yaml`
3. `--config` flag (error if set but missing)
4. `./shmorby.yaml` in current directory
5. Environment variables (`SHMORBY_PROVIDER`, `SHMORBY_MODEL`, etc.)
6. CLI flags (`--provider`, `--model`, `--agent` — always win)

See [`examples/shmorby.yaml`](examples/shmorby.yaml) for the full reference.

### Environment variables

| Variable | Purpose |
|----------|---------|
| `OPENROUTER_API_KEY` | OpenRouter API key |
| `OPENCODE_ZEN_API_KEY` | OpenCode Zen API key |
| `OPENCODE_ZEN_BASE_URL` | OpenCode Zen base URL (default `https://opencode.ai/zen`) |
| `OPENAI_API_KEY` | OpenAI API key |
| `OPENAI_ORG_ID` | OpenAI organization ID |
| `OPENAI_BASE_URL` | OpenAI / Azure base URL override |
| `OPENAI_TIMEOUT` | OpenAI HTTP client timeout in seconds |
| `OLLAMA_BASE_URL` | Ollama server URL (default `http://127.0.0.1:11434`) |
| `SHMORBY_PROVIDER` | Override default provider |
| `SHMORBY_MODEL` | Override default model |
| `SHMORBY_TOOLS_TIMEOUT` | Default tool timeout in seconds |
| `SHMORBY_TOOL_OUTPUT_MAX_LINES` | Cap tool output to N lines |
| `SHMORBY_TOOL_OUTPUT_MAX_BYTES` | Cap tool output to N bytes |

### CLI flags

```
shmorby [flags]

--provider string       LLM provider: openrouter, opencode_zen, openai, ollama (default "ollama")
--model string          Model name (default "llama3.2")
--config string         Config file path
--scope-file string     Operational context markdown (SCOPE.md)
--agent string          Agent mode: operate, diagnose (default "operate")
--system-prompt-file    Override system prompt file
--no-tui                Disable TUI, use plain stdin/stdout REPL
--log-level string      Log level: debug, info, warn, error (default "info")
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
| `/quit` | Exit shmorby |
| `/reset` | Clear conversation history |
| `/model` | Show current model |
| `/agent` | Show or switch agent mode |
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
| `shell` | allow | Runs without confirmation |
| `ssh` | allow | Runs without confirmation |
| `sudo` | ask | Requires approval; tool disabled by default (`tools.sudo.enabled: false`) |
| `aws` | ask | Requires approval; tool disabled by default (`tools.aws.enabled: false`) |

Set in the `permission` section of config. Options: `allow`, `ask`, `deny`.

### Permission presets

| Preset | shell | ssh | sudo | aws |
|--------|-------|-----|------|-----|
| full | allow | allow | ask | ask |
| read-only | allow | allow | deny | deny |
| locked | deny | deny | deny | deny |

### Blast radius

- **sudo** — requires `sudo -n` (non-interactive). Passwordless sudo must
  be configured.
- **ssh** — uses `BatchMode=yes` and `StrictHostKeyChecking=accept-new`.
  Requires SSH key-based auth.
- **aws** — uses the AWS CLI. Credentials from env/credentials file.

