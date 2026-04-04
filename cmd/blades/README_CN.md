# blades ⚡

> **基于文件系统的本地个人 AI Agent CLI**
>
> English: [README.md](README.md)

`blades` 是以本地文件为核心的命令行 AI 智能体：**blades 根目录（home）** 固定为 `~/.blades`，**Agent 工作空间** 默认为 `~/.blades/workspace`，也可通过 `--workspace` 指定任意路径。支持对话记忆、Markdown 技能、cron 定时任务及多 LLM 提供商。

---

## 功能特性

- **流式对话** — 带动画 spinner，逐 token 流式输出，支持 ANSI 彩色
- **持久记忆** — `MEMORY.md`（L1）、`memory/` 每日会话日志（L2）、知识文件（L3）
- **技能系统** — 在任意 skills 目录放入 `SKILL.md`，Agent 自动发现
- **Cron 调度** — 定时执行 Shell 或 Agent 对话，配置存于 `cron.yaml`
- **Daemon 模式** — 常驻进程运行调度器与可选通道（如 Lark）
- **多 LLM** — Anthropic、OpenAI、Google Gemini，在 `agent.yaml` 中切换

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
$EDITOR ~/.blades/agent.yaml
blades chat
```

---

## 目录结构

- **Blades 根目录（home）** — `~/.blades`。存放配置、cron、技能、会话及**运行日志**。
- **工作空间（workspace）** — 默认 `~/.blades/workspace`，或 `--workspace <path>`。Agent 工作目录（exec 默认 CWD、**记忆**、技能、输出）。

未指定 `--workspace` 时的默认布局：

```
~/.blades/                    ← 根目录（home）
├── agent.yaml
├── cron.yaml
├── skills/
├── sessions/
├── logs/                     ← 运行日志 YYYY-MM-DD.log
└── workspace/                 ← 默认工作空间
    ├── AGENTS.md, SOUL.md, IDENTITY.md, USER.md, MEMORY.md, TOOLS.md, HEARTBEAT.md
    ├── memory/                ← 每日会话日志（L2）YYYY-MM-DD.md
    ├── knowledges/
    └── outputs/
```

### 日志与记忆

- **日志** — 运行/审计日志写入 `~/.blades/logs/YYYY-MM-DD.log`（daemon 通道流量、错误等）。
- **记忆** — 当配置中 `logConversation: true` 时，用户/助手轮次会追加到**工作空间**的 `memory/YYYY-MM-DD.md`，用于长期上下文。

### `config.yaml`

```yaml
providers:
  - name: anthropic
    provider: anthropic
    models: [claude-sonnet-4-6]
    apiKey: ${ANTHROPIC_API_KEY}

# 可选：exec 工具覆盖（见模板）
# exec:
#   timeoutSeconds: 60
#   restrictToWorkspace: true

# 可选：blades daemon 的 Lark/飞书 通道（仅 WebSocket）
# channels:
#   lark:
#     enabled: true
#     appID: ${LARK_APP_ID}
#     appSecret: ${LARK_APP_SECRET}
#
# 可选：blades daemon 的微信 / iLink 通道（长轮询）
# channels:
#   weixin:
#     enabled: true
#     # accountID: user@im.wechat   # 只有 ~/.blades/weixin/account 下多账号时才需要指定
#     # botToken: ${WEIXIN_BOT_TOKEN}   # 不走扫码登录时可直接配置
#     # baseURL: https://ilinkai.weixin.qq.com
#     # routeTag: ""
#     # accountDir: ~/.blades/weixin/account
#     # stateDir: ~/.blades/weixin
#     # mediaDir: ~/.blades/weixin/media
#     # cdnBaseURL: https://your-cdn.example.com
#     # allowFrom: ["user@im.wechat"]
```

---

扫码登录流程：

```sh
blades weixin login
blades weixin list
```

默认目录结构：

- 账号：`~/.blades/weixin/account`
- sync：`~/.blades/weixin/sync`
- 媒体缓存：`~/.blades/weixin/media`

说明：

- `blades weixin login` 会扫码登录并把账号信息保存到本地；支持 `--base-url`、`--bot-type`、`--route-tag`、`--account-hint`、`--save-dir`、`--timeout`。
- `blades weixin list` 用来检查当前保存了哪些账号。
- 如果 `~/.blades/weixin/account` 下只有一个已保存账号，daemon 会自动选中它，一般不需要再手填 `accountID`。
- 如果保存了多个账号，需要显式设置 `channels.weixin.accountID`；如果不走扫码登录，也可以直接提供 `channels.weixin.botToken`。
- `stateDir` 默认是 `~/.blades/weixin`，实际同步文件写到 `stateDir/sync/<accountID>.sync.json`；`mediaDir` 默认为 `stateDir/media`。
- 配置了 `cdnBaseURL` 后，收到的图片/文件等媒体消息会下载到 `mediaDir`。

---

## 命令说明

### `blades init`

初始化 blades 根目录与工作空间。根目录固定为 `~/.blades`，工作空间为 `~/.blades/workspace` 或 `--workspace <path>`。

### `blades chat` / `blades run` / `blades memory` / `blades cron`

与英文 README 一致；支持 `--session`、cron 增删查运行、heartbeat 等。

补充说明：
- `blades cron add` 的 `--cron`、`--every`、`--delay` 三者必须且只能传一个。
- `blades cron add` 现在明确支持三种任务类型：`--type exec`、`--type agent`、`--type notify`。
- 执行终端命令用 `--command`，让助手处理任务用 `--prompt`，直接发到社交/聊天软件用 `--text --chat-session`。
- `--chat-session` 对 `exec` 和 `agent` 是可选的；设置后，任务结果会主动推送到对应聊天会话。
- 定时 `agent_turn` 任务会持久化 session，因此 heartbeat 和周期任务在 daemon 重启后仍能保持对话连续性。
- `blades cron heartbeat --every 15m` 会复用已有 heartbeat 任务并更新调度周期，而不是静默保留旧配置。

### `blades daemon`

以常驻进程运行 cron 与可选通道（如 Lark、微信/iLink）。Lark 仅支持 **WebSocket**，飞书后台选择「通过长连接接收事件」；微信通道走 iLink **长轮询**，推荐先执行 `blades weixin login` 扫码，默认从 `~/.blades/weixin/account` 读取账号，在 `~/.blades/weixin/sync` 下维护同步状态，并可用 `allowFrom` 限制允许接入的微信用户。

### `blades doctor`

检查 blades 根目录、工作空间、必要文件及过期 cron 任务。传入 `--config` 时，会按实际配置文件路径检查；如果启用了微信通道，还会检查账号目录、已保存账号数量，以及是否已经能确定 `accountID` / `botToken`。

---

## 全局标志

| 标志 | 默认值 | 说明 |
|------|--------|------|
| `--config` | `~/.blades/config.yaml` | 配置文件路径 |
| `--workspace` | `~/.blades/workspace` | 工作空间路径 |
| `--debug` | false | 详细调试日志 |

---

## 技能

- **技能**：从 `~/.blades/skills/` 加载（全局）。

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
