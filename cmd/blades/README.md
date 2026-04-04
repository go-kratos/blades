# blades ⚡

> **A file-system-based personal AI agent CLI**
>
> 中文文档：[README_CN.md](README_CN.md)

`blades` is a local-first command-line AI agent backed by plain files: **blades home (root)** at `~/.blades` and an **agent workspace** at `~/.blades/workspace` by default (or any path via `--workspace`). It remembers your conversations, loads skills from Markdown, schedules recurring tasks via cron, and supports multiple LLM providers.

---

## Features

- **Streaming chat** — animated spinner → token-by-token streaming with ANSI colour support
- **Persistent memory** — `MEMORY.md` (L1), daily session logs in `memory/` (L2), knowledge files (L3)
- **Skill system** — drop a `SKILL.md` into any skills directory and the agent picks it up automatically
- **Cron scheduler** — run shell commands or agent turns on a schedule; backed by `cron.yaml`
- **Daemon mode** — keep the scheduler running as a long-lived background process
- **Multi-provider** — Anthropic Claude, OpenAI, Google Gemini; switch in `agent.yaml`

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

# 3. Edit config and agent recipe
$EDITOR ~/.blades/config.yaml
$EDITOR ~/.blades/agent.yaml

# 4. Start chatting
blades chat
```

---

## Directory layout

- **Blades home (root)** — `~/.blades`. Holds config, cron, skills, sessions, and **runtime logs**.
- **Workspace** — `~/.blades/workspace` by default, or `--workspace <path>`. The agent’s working directory (exec CWD, **memory**, skills, outputs).

Default layout when no custom `--workspace` is set:

```
~/.blades/                    ← home (root)
├── agent.yaml                ← LLM provider, model, API key
├── cron.yaml                 ← persistent cron job store
├── skills/                   ← global skills (all workspaces)
├── sessions/                 ← conversation state per session ID
├── logs/                     ← runtime logs (YYYY-MM-DD.log)
└── workspace/                ← agent workspace (default)
    ├── AGENTS.md             ← behaviour rules (loaded at startup)
    ├── SOUL.md, IDENTITY.md, USER.md, MEMORY.md, TOOLS.md, HEARTBEAT.md
    ├── memory/               ← daily session logs (L2) — YYYY-MM-DD.md
    ├── knowledges/           ← reference knowledge (L3)
    └── outputs/              ← agent-produced files
```

### Log vs memory

- **Log** — runtime/audit logs go to `~/.blades/logs/YYYY-MM-DD.log` (daemon channel traffic, errors).
- **Memory** — when `logConversation: true` in config, user/assistant turns are appended to **workspace** `memory/YYYY-MM-DD.md` for long-term context.

### `config.yaml`

```yaml
providers:
  - name: anthropic
    provider: anthropic
    models: [claude-sonnet-4-6]
    apiKey: ${ANTHROPIC_API_KEY} # env-var expansion supported

# Optional: exec tool overrides (see template)
# exec:
#   timeoutSeconds: 60
#   restrictToWorkspace: true

# Optional: Lark/Feishu channel (WebSocket only) for `blades daemon`
# channels:
#   lark:
#     enabled: true
#     appID: ${LARK_APP_ID}
#     appSecret: ${LARK_APP_SECRET}
#
# Optional: Weixin/iLink channel (long polling) for `blades daemon`
# channels:
#   weixin:
#     enabled: true
#     # accountID: user@im.wechat
#     # botToken: ${WEIXIN_BOT_TOKEN}   # optional when not using QR login
#     # baseURL: https://ilinkai.weixin.qq.com
#     # routeTag: ""
#     # accountDir: ~/.blades/weixin/account
#     # stateDir: ~/.blades/weixin
#     # mediaDir: ~/.blades/weixin/media
#     # cdnBaseURL: https://your-cdn.example.com
#     # allowFrom: ["user@im.wechat"]
```

---

Scan-login flow:

```sh
blades weixin login
blades weixin list
```

By default blades stores Weixin files here:

- accounts: `~/.blades/weixin/account`
- sync state: `~/.blades/weixin/sync`
- media cache: `~/.blades/weixin/media`

Notes:

- `blades weixin login` performs QR-code login and persists the account locally; it supports `--base-url`, `--bot-type`, `--route-tag`, `--account-hint`, `--save-dir`, and `--timeout`.
- `blades weixin list` shows the saved local accounts.
- If `~/.blades/weixin/account` contains exactly one saved account, the daemon selects it automatically, so `accountID` is usually unnecessary.
- If you keep multiple saved accounts, set `channels.weixin.accountID`. If you do not use QR login, you can also provide `channels.weixin.botToken` directly.
- `stateDir` defaults to `~/.blades/weixin`, and the sync buffer is written to `stateDir/sync/<accountID>.sync.json`; `mediaDir` defaults to `stateDir/media`.
- When `cdnBaseURL` is configured, inbound media files are downloaded into `mediaDir`.

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

**In-chat slash commands:** `/help`, `/session <id>`, `/clear`, `/exit`.

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
blades cron add --name "morning-brief" --type agent --cron "0 8 * * *" --prompt "generate my morning brief"
blades cron add --name "health-check" --type exec --every 1h --command "echo ok"
blades cron add --name "post-reminder" --type notify --every 1h --text "remember to post" --chat-session "chat-id"
blades cron add --name "test ls" --type exec --delay 10 --command "ls . > outputs/test.txt"
blades cron heartbeat
blades cron heartbeat --every 15m   # reuses the heartbeat job and updates its schedule
blades cron heartbeat --run-now
blades cron remove <id>
blades cron run <id>
```

Notes:
- `blades cron add` requires exactly one schedule flag: `--cron`, `--every`, or `--delay`.
- `blades cron add` supports three task types: `--type exec`, `--type agent`, and `--type notify`.
- Use `--command` for terminal execution, `--prompt` for assistant tasks, and `--text --chat-session` for direct chat delivery.
- `--chat-session` is optional for `exec` and `agent` jobs; when set, the job output is pushed to that chat/session.
- Scheduled `agent_turn` jobs persist their session history, so recurring jobs and `heartbeat` keep continuity across daemon restarts.

### `blades daemon`

Run the cron scheduler and optional channels (e.g. Lark or Weixin) as a long-lived process.

```sh
blades daemon
# systemd example: ExecStart=/usr/local/bin/blades daemon --workspace /home/user/my-agent
```

Lark uses **WebSocket only**. In Feishu, choose “Receive events through persistent connection” (no request URL). Weixin uses iLink **long polling** and is intended to be used via QR-code login (`blades weixin login`). By default it reads accounts from `~/.blades/weixin/account` and writes sync state to `~/.blades/weixin/sync`. `accountID` is only needed when you keep multiple saved accounts in that account directory.

### `blades doctor`

Check health: blades home, workspace directory, required files, stale cron jobs.

```sh
blades doctor
blades doctor --config ~/custom-blades-config.yaml
```

When the Weixin channel is enabled, `blades doctor` also checks the account directory, saved account count, and whether `accountID` / `botToken` can be resolved.

---

## Global flags

| Flag | Default | Description |
|------|---------|-------------|
| `--config` | `~/.blades/config.yaml` | Path to config file |
| `--workspace` | `~/.blades/workspace` | Agent workspace directory |
| `--debug` | false | Verbose debug logging |

---

## Skill system

Skills are discovered from `~/.blades/skills/` (global):

| Directory | Scope |
|-----------|--------|
| `~/.blades/skills/` | Global blades |

Each skill is a directory with a `SKILL.md` (YAML front-matter + body).

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
