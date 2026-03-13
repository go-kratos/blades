# blades ⚡

> **A file-system-based personal AI agent CLI**
> 
> 中文文档：[README_CN.md](README_CN.md)

`blades` is a local-first command-line AI agent backed by a plain-file workspace (`~/.blades`). It remembers your conversations, loads skills from Markdown, schedules recurring tasks via cron, and supports any LLM provider — Anthropic, OpenAI, or Gemini.

---

## Features

- **Streaming chat** — animated spinner → token-by-token streaming with ANSI colour support
- **Persistent memory** — `MEMORY.md` (L1), daily session logs (L2), knowledge files (L3)
- **Skill system** — drop a `SKILL.md` into `skills/<name>/` and the agent picks it up automatically
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
# 1. Initialise workspace (creates ~/.blades with template files)
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
├── config.yaml      ← LLM provider, model, defaults
├── SOUL.md          ← agent's core personality
├── IDENTITY.md      ← role and current mission
├── AGENTS.md        ← behaviour rules (similar to AGENTS.md / CLAUDE.md)
├── USER.md          ← facts about you the agent should always know
├── MEMORY.md        ← long-term distilled memory (L1)
├── skills/
│   ├── git-backup/SKILL.md
│   └── distill/SKILL.md
├── memories/        ← daily session logs (L2) — YYYY-MM-DD.md
├── knowledges/      ← reference knowledge files the agent can load when needed (L3)
├── sessions/        ← conversation state per session ID
├── outputs/         ← agent-produced files
└── cron.json        ← persistent cron job store
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

Initialise the workspace. Creates `~/.blades` with all template files and built-in skills (`git-backup`, `distill`). Safe to re-run — existing files are never overwritten.

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
```

**In-chat slash commands:**

| Command | Description |
|---|---|
| `/help` | Show available commands |
| `/reload` | Hot-reload skills without restarting |
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
blades memory search "last week"       # search session logs
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

# Ensure a heartbeat job exists; skip creation if it is already there
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
| `--config` | workspace `config.yaml` | Path to a custom config file |
| `--workspace` | `~/.blades` | Path to workspace root |

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

Drop the directory into `~/.blades/skills/` and type `/reload` in chat (or restart). The agent will automatically discover and use the new skill.

Built-in skills installed by `blades init`:

| Skill | Description |
|---|---|
| `git-backup` | Stage, commit, and push all workspace changes |
| `distill` | Analyse session logs and distill lessons into `MEMORY.md` |

---

## Memory Architecture

By default, blades only injects AGENTS.md into the initial system instruction.
The agent should follow AGENTS.md to read SOUL.md, USER.md, MEMORY.md,
recent logs, and knowledges/ at runtime when needed.

| Layer | File | Description |
|---|---|---|
| L1 | `MEMORY.md` | Manually curated or distilled facts the agent should read in direct sessions |
| L2 | `memories/YYYY-MM-DD.md` | Append-only daily session logs for recent context |
| L3 | `knowledges/*.md` | Reference files the agent can load on demand |

---

## License

Apache 2.0 — see [LICENSE](../../LICENSE).
