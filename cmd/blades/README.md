# blades ⚡

> **A file-system-based personal AI agent CLI**
>
> 中文文档：[README_CN.md](README_CN.md)

`blades` is a local-first command-line AI agent backed by a plain-file workspace (`~/.blades`). It remembers your conversations, loads skills from Markdown, connects to MCP tool servers, schedules recurring tasks via cron, and supports any LLM provider — Anthropic, OpenAI, or Gemini.

---

## Features

- **Streaming chat** — animated spinner → token-by-token streaming with ANSI colour support
- **Persistent memory** — `MEMORY.md` (L1), daily session logs in `memory/` (L2), knowledge files (L3)
- **Skill system** — drop a `SKILL.md` into any skills directory and the agent picks it up automatically
- **MCP support** — connect external tool servers via `mcp.json` (stdio, HTTP, WebSocket)
- **Cron scheduler** — run shell commands or agent turns on a schedule; backed by `cron.json`
- **Daemon mode** — keep the scheduler running as a long-lived background process
- **Multi-provider** — Anthropic Claude, OpenAI, Google Gemini; switch in `config.yaml`
- **Hot reload** — type `/reload` in chat to pick up skill or config changes without restarting

---

## Installation

```sh
# From source (requires Go 1.24+)
git clone https://github.com/go-kratos/blades
cd blades/cmd/blades
go install .
```

---

## Quick Start

```sh
# 1. Initialise workspace (creates ~/.blades with all template files)
blades init

# 2. Set your API key
export ANTHROPIC_API_KEY=sk-...     # or OPENAI_API_KEY / GEMINI_API_KEY

# 3. Edit config (choose provider + model)
$EDITOR ~/.blades/config.yaml

# 4. Start chatting
blades chat
```

---

## Workspace Layout

```
~/.blades/
├── config.yaml          ← LLM provider, model, API key
├── mcp.json             ← global MCP server connections
├── cron.json            ← persistent cron job store
├── skills/              ← global skills (available to all workspaces)
├── sessions/            ← conversation state per session ID
└── workspace/           ← agent operating directory (default exec CWD)
    ├── AGENTS.md        ← behaviour rules loaded at every session start
    ├── SOUL.md          ← agent's core personality
    ├── IDENTITY.md      ← role, capabilities, quick-reference card
    ├── USER.md          ← facts about you the agent should always know
    ├── MEMORY.md        ← long-term distilled memory (L1)
    ├── TOOLS.md         ← machine-specific setup notes
    ├── HEARTBEAT.md     ← proactive check-in task list
    ├── mcp.json         ← workspace-level MCP server connections
    ├── skills/          ← workspace-local skills
    ├── memory/          ← daily session logs (L2) — YYYY-MM-DD.md
    ├── knowledges/      ← reference knowledge files (L3)
    └── outputs/         ← agent-produced files
```

### config.yaml

```yaml
llm:
  provider: anthropic          # anthropic | openai | gemini
  model: claude-sonnet-4-6
  apiKey: ${ANTHROPIC_API_KEY} # env-var expansion supported

defaults:
  maxIterations: 10
  compressThreshold: 40000
```

---

## Commands

### `blades init`

Initialise the workspace. Creates `~/.blades` with all template files and directories. Safe to re-run — existing files are never overwritten.

```sh
blades init
blades init --workspace ~/work-agent   # custom workspace path
blades init --git                      # also run git init + create .gitignore
```

### `blades chat`

Start an interactive streaming conversation.

```sh
blades chat
blades chat --session my-project       # resume or start a named session
blades chat --simple                   # plain line I/O (fixes Windows IME issues)
```

**In-chat slash commands:**

| Command | Description |
|---|---|
| `/help` | Show available commands |
| `/reload` | Hot-reload skills and config without restarting |
| `/session <id>` | Switch to a different session |
| `/clear` | Clear the terminal screen |
| `/exit` | Quit |

### `blades run`

Execute a single agent turn and exit (useful for scripts and cron).

```sh
blades run --message "summarise today's notes"
blades run -m "@distill"
blades run -m "write morning report" --session reports
```

### `blades memory`

```sh
blades memory show                     # print MEMORY.md
blades memory add "prefer short answers"
blades memory search "last week"       # search session logs in memory/
```

### `blades cron`

```sh
# List all active jobs
blades cron list
blades cron list --all                 # include disabled jobs

# Add a job (agent turn on schedule)
blades cron add --name "morning-brief" \
  --cron "0 8 * * *" \
  --message "generate my morning brief"

# Add a job (shell command every hour)
blades cron add --name "health-check" --every 1h --command "echo ok"

# Ensure a heartbeat job exists; skip creation if already present
blades cron heartbeat

# Ensure the heartbeat job exists and trigger it immediately once
blades cron heartbeat --run-now

# Remove a job by ID
blades cron remove <id>

# Run a job immediately
blades cron run <id>
```

### `blades daemon`

Run the cron scheduler as a persistent process.

```sh
blades daemon

# As a systemd service — example unit file:
# ExecStart=/usr/local/bin/blades daemon --workspace /home/user/.blades
```

### `blades doctor`

Check workspace health: verify required files exist, report stale cron jobs.

```sh
blades doctor
```

---

## Global Flags

| Flag | Default | Description |
|---|---|---|
| `--config` | `~/.blades/config.yaml` | Path to a custom config file |
| `--workspace` | `~/.blades` | Path to the blades root directory |
| `--debug` | false | Enable verbose debug logging |

---

## Skill System

A skill is a directory containing a `SKILL.md` file with a YAML front-matter header:

```
skills/
└── my-skill/
    └── SKILL.md
```

```markdown
---
name: my-skill
description: Does something useful for the agent.
---

# My Skill

Instructions for the agent go here…
```

Skills are discovered from three directories in order — later directories can shadow earlier ones:

| Directory | Scope |
|---|---|
| `~/.agents/skills/` | System-wide (shared across tools) |
| `~/.blades/skills/` | Global blades skills |
| `~/.blades/workspace/skills/` | Workspace-local skills |

Type `/reload` in chat (or restart) to pick up new skills.

Built-in skill installed by `blades init`:

| Skill | Description |
|---|---|
| `blades-cron` | Global skill installed in `~/.blades/skills/` for scheduling shell commands or agent turns from within chat |

---

## MCP (Model Context Protocol)

blades supports connecting to external MCP tool servers. Servers are configured in `mcp.json` files using the same schema as Claude Desktop, so existing configs can be reused directly.

MCP servers are loaded from two files and merged together:

| File | Scope |
|---|---|
| `~/.blades/mcp.json` | Global (all workspaces) |
| `~/.blades/workspace/mcp.json` | This workspace only |

Additional servers can also be declared inline in `config.yaml` under the `mcp:` key.

### mcp.json format

```json
{
  "mcpServers": {
    "time": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-time"]
    },
    "filesystem": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"],
      "env": { "DEBUG": "1" }
    },
    "my-api": {
      "transport": "http",
      "endpoint": "http://localhost:8080/mcp",
      "headers": { "Authorization": "Bearer ${MY_TOKEN}" },
      "timeoutSeconds": 15
    }
  }
}
```

`transport` defaults to `stdio` when omitted. String values support `${ENV_VAR}` expansion.

---

## Memory Architecture

By default, blades injects only `AGENTS.md` into the system instruction. `AGENTS.md` then instructs the agent which files to read at runtime (SOUL.md, USER.md, MEMORY.md, recent logs, knowledges/).

| Layer | Location | Description |
|---|---|---|
| L1 | `workspace/MEMORY.md` | Curated long-term facts; read in direct sessions |
| L2 | `workspace/memory/YYYY-MM-DD.md` | Append-only daily session logs |
| L3 | `workspace/knowledges/*.md` | Reference files loaded on demand |

---

## License

Apache 2.0 — see [LICENSE](../../LICENSE).
