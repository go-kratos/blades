# blades ⚡

> **基于文件系统的本地个人 AI Agent CLI**
>
> English: [README.md](README.md)

`blades` 是以本地文件为核心的命令行 AI 智能体：**blades 根目录（home）** 固定为 `~/.blades`，**Agent 工作空间** 默认为 `~/.blades/workspace`，也可通过 `--workspace` 指定任意路径。支持对话记忆、Markdown 技能、通过 `mcp.json` 连接 MCP 工具、cron 定时任务及多 LLM 提供商。

---

## 功能特性

- **流式对话** — 带动画 spinner，逐 token 流式输出，支持 ANSI 彩色
- **持久记忆** — `MEMORY.md`（L1）、`memory/` 每日会话日志（L2）、知识文件（L3）
- **技能系统** — 在任意 skills 目录放入 `SKILL.md`，Agent 自动发现
- **MCP 支持** — 仅通过 `mcp.json` 连接外部工具（stdio、HTTP、WebSocket）
- **Cron 调度** — 定时执行 Shell 或 Agent 对话，配置存于 `cron.json`
- **Daemon 模式** — 常驻进程运行调度器与可选通道（如 Lark）
- **多 LLM** — Anthropic、OpenAI、Google Gemini，在 `config.yaml` 中切换
- **热重载** — 对话中输入 `/reload` 即可加载新技能或配置

---

## 安装

```sh
git clone https://github.com/go-kratos/blades
cd blades/cmd/blades
go install .
```

---

## 快速开始

```sh
blades init
export ANTHROPIC_API_KEY=sk-...
$EDITOR ~/.blades/config.yaml
blades chat
```

---

## 目录结构

- **Blades 根目录（home）** — `~/.blades`。存放配置、全局 MCP、cron、技能、会话及**运行日志**。
- **工作空间（workspace）** — 默认 `~/.blades/workspace`，或 `--workspace <path>`。Agent 工作目录（exec 默认 CWD、工作区级 MCP、**记忆**、技能、输出）。

未指定 `--workspace` 时的默认布局：

```
~/.blades/                    ← 根目录（home）
├── config.yaml
├── mcp.json
├── cron.json
├── skills/
├── sessions/
├── log/                      ← 运行日志 YYYY-MM-DD.log
└── workspace/                 ← 默认工作空间
    ├── AGENTS.md, SOUL.md, IDENTITY.md, USER.md, MEMORY.md, TOOLS.md, HEARTBEAT.md
    ├── mcp.json
    ├── skills/
    ├── memory/                ← 每日会话日志（L2）YYYY-MM-DD.md
    ├── knowledges/
    └── outputs/
```

### 日志与记忆

- **日志** — 运行/审计日志写入 `~/.blades/log/YYYY-MM-DD.log`（daemon 通道流量、错误等）。
- **记忆** — 当配置中 `logConversation: true` 时，用户/助手轮次会追加到**工作空间**的 `memory/YYYY-MM-DD.md`，用于长期上下文。

### config.yaml

```yaml
llm:
  provider: anthropic
  model: claude-sonnet-4-6
  apiKey: ${ANTHROPIC_API_KEY}

defaults:
  maxIterations: 10
  compressThreshold: 40000
  logConversation: false   # 为 true 时，将对话追加到 workspace/memory/YYYY-MM-DD.md

# 可选：exec 工具覆盖（见模板）
# exec:
#   timeoutSeconds: 60
#   restrictToWorkspace: true

# 可选：blades daemon 的 Lark/飞书 通道（仅 WebSocket）
# lark:
#   enabled: true
#   appID: ${LARK_APP_ID}
#   appSecret: ${LARK_APP_SECRET}
```

---

## 命令说明

### `blades init`

初始化 blades 根目录与工作空间。根目录固定为 `~/.blades`，工作空间为 `~/.blades/workspace` 或 `--workspace <path>`。

### `blades chat` / `blades run` / `blades memory` / `blades cron`

与英文 README 一致；支持 `--session`、`/reload`、cron 增删查运行、heartbeat 等。

### `blades daemon`

以常驻进程运行 cron 与可选通道（如 Lark）。Lark 仅支持 **WebSocket**，飞书后台选择「通过长连接接收事件」。

### `blades doctor`

检查 blades 根目录、工作空间、必要文件及过期 cron 任务。

---

## 全局标志

| 标志 | 默认值 | 说明 |
|------|--------|------|
| `--config` | `~/.blades/config.yaml` | 配置文件路径 |
| `--workspace` | `~/.blades/workspace` | 工作空间路径 |
| `--debug` | false | 详细调试日志 |

---

## 技能与 MCP

- **技能**：按顺序从 `~/.agents/skills/`、`~/.blades/skills/`、`<workspace>/skills/` 合并加载。
- **MCP**：仅通过 **mcp.json** 配置（不支持在 config.yaml 内联）。合并顺序：`~/.blades/mcp.json` → `<workspace>/mcp.json`。格式与 Claude Desktop 兼容，支持 `${ENV_VAR}` 展开。

---

## 记忆架构

| 层级 | 位置 | 说明 |
|------|------|------|
| L1 | `workspace/MEMORY.md` | 长期事实 |
| L2 | `workspace/memory/YYYY-MM-DD.md` | 每日会话日志（及可选的对话记录） |
| L3 | `workspace/knowledges/*.md` | 按需参考文件 |

---

## 许可证

Apache 2.0 — 见 [LICENSE](../../LICENSE)。
