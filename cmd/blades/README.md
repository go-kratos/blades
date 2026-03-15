# blades ⚡

> **A file-system-based personal AI agent CLI**
>
> 中文文档：[README_CN.md](README_CN.md)

`blades` is a local-first command-line AI agent backed by plain files: **blades home (root)** at `~/.blades` and an **agent workspace** at `~/.blades/workspace` by default (or any path via `--workspace`). It remembers your conversations, loads skills from Markdown, connects to MCP tool servers via `mcp.json`, schedules recurring tasks via cron, and supports multiple LLM providers.

---

## Features

- **Streaming chat** — animated spinner → token-by-token streaming with ANSI colour support
- **Persistent memory** — `MEMORY.md` (L1), daily session logs in `memory/` (L2), knowledge files (L3)
- **Skill system** — drop a `SKILL.md` into any skills directory and the agent picks it up automatically
- **MCP support** — connect external tool servers via `mcp.json` only (stdio, HTTP, WebSocket)
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

## Directory layout

- **Blades home (root)** — `~/.blades`. Holds config, global MCP, cron, skills, sessions, and **runtime logs**.
- **Workspace** — `~/.blades/workspace` by default, or `--workspace <path>`. The agent’s working directory (exec CWD, workspace-level MCP, **memory**, skills, outputs).

Default layout when no custom `--workspace` is set:

```
~/.blades/                    ← home (root)
├── config.yaml               ← LLM provider, model, API key
├── mcp.json                  ← global MCP server connections
├── cron.json                 ← persistent cron job store
├── skills/                   ← global skills (all workspaces)
├── sessions/                 ← conversation state per session ID
├── log/                      ← runtime logs (YYYY-MM-DD.log)
└── workspace/                ← agent workspace (default)
    ├── AGENTS.md             ← behaviour rules (loaded at startup)
    ├── SOUL.md, IDENTITY.md, USER.md, MEMORY.md, TOOLS.md, HEARTBEAT.md
    ├── mcp.json              ← workspace-level MCP servers
    ├── skills/               ← workspace-local skills
    ├── memory/               ← daily session logs (L2) — YYYY-MM-DD.md
    ├── knowledges/           ← reference knowledge (L3)
    └── outputs/              ← agent-produced files
```

### Log vs memory

- **Log** — runtime/audit logs go to `~/.blades/log/YYYY-MM-DD.log` (daemon channel traffic, errors).
- **Memory** — when `logConversation: true` in config, user/assistant turns are appended to **workspace** `memory/YYYY-MM-DD.md` for long-term context.

### config.yaml

```yaml
llm:
  provider: anthropic          # anthropic | openai | gemini
  model: claude-sonnet-4-6
  apiKey: ${ANTHROPIC_API_KEY} # env-var expansion supported

defaults:
  maxIterations: 10
  compressThreshold: 40000
  logConversation: false      # when true, append turns to workspace/memory/YYYY-MM-DD.md

# Optional: exec tool overrides (see template)
# exec:
#   timeoutSeconds: 60
#   restrictToWorkspace: true

# Optional: Lark/Feishu channel (WebSocket only) for `blades daemon`
# lark:
#   enabled: true
#   appID: ${LARK_APP_ID}
#   appSecret: ${LARK_APP_SECRET}
```

---

## Commands

### `blades init`

Initialise blades home and workspace. Home is always `~/.blades`; workspace is `~/.blades/workspace` or `--workspace <path>`. Safe to re-run — existing files are never overwritten.

```sh
blades init
blades init --workspace ~/work-agent
blades init --git
```

### `blades chat`

Start an interactive streaming conversation.

```sh
blades chat
blades chat --session my-project
blades chat --simple
```

**In-chat slash commands:** `/help`, `/reload`, `/session <id>`, `/clear`, `/exit`.

### `blades run`

Execute a single agent turn and exit.

```sh
blades run --message "summarise today's notes"
blades run -m "@distill" --session reports
```

### `blades memory`

```sh
blades memory show
blades memory add "prefer short answers"
blades memory search "last week"
```

### `blades cron`

```sh
blades cron list
blades cron list --all
blades cron add --name "morning-brief" --cron "0 8 * * *" --message "generate my morning brief"
blades cron add --name "health-check" --every 1h --command "echo ok"
blades cron add --name "test ls" --delay 10 --command "ls . > outputs/test.txt"
blades cron heartbeat
blades cron heartbeat --run-now
blades cron remove <id>
blades cron run <id>
```

### `blades daemon`

Run the cron scheduler and optional channels (e.g. Lark) as a long-lived process.

```sh
blades daemon
# systemd example: ExecStart=/usr/local/bin/blades daemon --workspace /home/user/my-agent
```

Lark uses **WebSocket only**. In Feishu, choose “Receive events through persistent connection” (no request URL).

### `blades doctor`

Check health: blades home, workspace directory, required files, stale cron jobs.

```sh
blades doctor
```

---

## Global flags

| Flag | Default | Description |
|------|---------|-------------|
| `--config` | `~/.blades/config.yaml` | Path to config file |
| `--workspace` | `~/.blades/workspace` | Agent workspace directory |
| `--debug` | false | Verbose debug logging |

---

## Skill system

Skills are discovered from three directories in order (later does not override; all are merged):

| Directory | Scope |
|-----------|--------|
| `~/.agents/skills/` | System-wide |
| `~/.blades/skills/` | Global blades |
| `<workspace>/skills/` | Workspace-local |

Each skill is a directory with a `SKILL.md` (YAML front-matter + body). Type `/reload` in chat to pick up changes.

---

## MCP (Model Context Protocol)

MCP servers are configured **only in `mcp.json`** (not in `config.yaml`). Two files are merged:

| File | Scope |
|------|--------|
| `~/.blades/mcp.json` | Global |
| `<workspace>/mcp.json` | Workspace |

Same schema as Claude Desktop. String values support `${ENV_VAR}` expansion.

---

## Memory architecture

| Layer | Location | Description |
|-------|----------|-------------|
| L1 | `workspace/MEMORY.md` | Long-term facts |
| L2 | `workspace/memory/YYYY-MM-DD.md` | Daily session logs (and optional conversation when `logConversation: true`) |
| L3 | `workspace/knowledges/*.md` | Reference files on demand |

---

## License

Apache 2.0 — see [LICENSE](../../LICENSE).
