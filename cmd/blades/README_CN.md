# blades ⚡

> **基于文件系统的本地个人 AI Agent CLI**
>
> English documentation: [README.md](README.md)

`blades` 是一个以本地文件为核心的命令行 AI 智能体：全局 home 目录位于 `~/.blades`，Agent 工作空间默认位于 `~/.blades/workspace`（也可通过 `--workspace` 指向任意目录）。它能记住你的对话、从 Markdown 文件加载技能、通过 MCP 连接外部工具服务器、通过 cron 调度定时任务，并支持多种 LLM 提供商——Anthropic、OpenAI 和 Gemini。

---

## 功能特性

- **流式对话** — 带动画 spinner，逐 token 流式输出，支持 ANSI 彩色终端
- **持久记忆** — `MEMORY.md`（L1）、`memory/` 每日对话日志（L2）、知识文件（L3）
- **技能系统** — 在任意 skills 目录放入 `SKILL.md`，Agent 自动发现并使用
- **MCP 支持** — 通过 `mcp.json` 连接外部工具服务器（stdio、HTTP、WebSocket）
- **Cron 调度** — 定时执行 Shell 命令或 Agent 对话，配置存储于 `cron.json`
- **Daemon 模式** — 作为常驻后台进程运行调度器
- **多 LLM 支持** — Anthropic Claude、OpenAI、Google Gemini，在 `config.yaml` 中切换
- **热重载** — 在对话中输入 `/reload`，无需重启即可加载新技能或配置变更

---

## 安装

```sh
# 从源码安装（需要 Go 1.24+）
git clone https://github.com/go-kratos/blades
cd blades/cmd/blades
go install .
```

---

## 快速开始

```sh
# 1. 初始化工作空间（在 ~/.blades 创建所有模板文件和目录）
blades init

# 2. 设置 API Key
export ANTHROPIC_API_KEY=sk-...     # 或 OPENAI_API_KEY / GEMINI_API_KEY

# 3. 编辑配置（选择提供商和模型）
$EDITOR ~/.blades/config.yaml

# 4. 开始对话
blades chat
```

---

## 工作空间结构

默认结构（未配置自定义 workspace 路径时）：

```
~/.blades/
├── config.yaml          ← LLM 提供商、模型、API Key
├── mcp.json             ← 全局 MCP 服务器连接配置
├── cron.json            ← 持久化的 cron 任务存储
├── skills/              ← 全局技能目录（所有工作空间共享）
├── sessions/            ← 按 session ID 保存的对话状态
└── workspace/           ← Agent 工作目录（exec 工具默认在此执行）
    ├── AGENTS.md        ← 行为准则（每次会话启动时加载）
    ├── SOUL.md          ← Agent 的核心人格
    ├── IDENTITY.md      ← 角色定义、能力说明、快速参考卡
    ├── USER.md          ← 关于你，Agent 应始终了解的事
    ├── MEMORY.md        ← 长期浓缩记忆（L1）
    ├── TOOLS.md         ← 本机特定的配置说明
    ├── HEARTBEAT.md     ← 主动定时检查任务列表
    ├── mcp.json         ← 工作空间级别的 MCP 服务器配置
    ├── skills/          ← 工作空间本地技能
    ├── memory/          ← 每日对话日志（L2） — YYYY-MM-DD.md
    ├── knowledges/      ← 按需加载的知识文件（L3）
    └── outputs/         ← Agent 生成的文件
```

### config.yaml

```yaml
llm:
  provider: anthropic          # anthropic | openai | gemini
  model: claude-sonnet-4-6
  apiKey: ${ANTHROPIC_API_KEY} # 支持环境变量展开

defaults:
  maxIterations: 10
  compressThreshold: 40000
```

---

## 命令说明

### `blades init`

初始化 blades 的 home 与 workspace 目录。home 级文件固定创建在 `~/.blades`；workspace 级文件创建在 `~/.blades/workspace`（或 `--workspace` 指定目录）。可重复执行——已有文件不会被覆盖。

```sh
blades init
blades init --workspace ~/work-agent   # 指定自定义工作空间路径
blades init --git                      # 同时执行 git init 并创建 .gitignore
```

### `blades chat`

启动流式交互对话。

```sh
blades chat
blades chat --session my-project       # 恢复或创建指定名称的会话
blades chat --simple                   # 纯文本行模式（解决 Windows 输入法问题）
```

**对话内置指令：**

| 指令 | 说明 |
|---|---|
| `/help` | 显示可用指令 |
| `/reload` | 热重载技能和配置，无需重启 |
| `/session <id>` | 切换到不同的会话 |
| `/clear` | 清空终端屏幕 |
| `/exit` | 退出 |

### `blades run`

执行单轮 Agent 对话后退出（适合脚本和 cron 使用）。

```sh
blades run --message "总结今天的笔记"
blades run -m "@distill"
blades run -m "生成晨报" --session reports
```

### `blades memory`

```sh
blades memory show                     # 打印 MEMORY.md 内容
blades memory add "偏好简洁的回答"
blades memory search "上周"            # 搜索 memory/ 目录下的对话日志
```

### `blades cron`

```sh
# 列出所有活跃任务
blades cron list
blades cron list --all                 # 包含已禁用任务

# 添加 Agent 对话定时任务
blades cron add --name "morning-brief" \
  --cron "0 8 * * *" \
  --message "生成我的晨报"

# 添加 Shell 命令定时任务（每小时执行）
blades cron add --name "health-check" --every 1h --command "echo ok"

# 添加一次性任务（10 秒后执行）
blades cron add --name "test ls" --delay 10 --command "ls . > outputs/test.txt"

# 确保心跳任务存在；如果已经存在则跳过创建
blades cron heartbeat

# 确保心跳任务存在，并立即手动触发一次
blades cron heartbeat --run-now

# 按 ID 删除任务
blades cron remove <id>

# 立即执行某个任务
blades cron run <id>
```

### `blades daemon`

将 cron 调度器作为常驻进程运行。

```sh
blades daemon

# 作为 systemd 服务的示例单元文件：
# ExecStart=/usr/local/bin/blades daemon --workspace /home/user/my-agent
```

### `blades doctor`

检查工作空间健康状态：验证必要文件是否存在，报告过期的 cron 任务。

```sh
blades doctor
```

---

## 全局标志

| 标志 | 默认值 | 说明 |
|---|---|---|
| `--config` | `~/.blades/config.yaml` | 自定义配置文件路径 |
| `--workspace` | `~/.blades/workspace` | Agent 工作空间目录路径 |
| `--debug` | false | 启用详细调试日志 |

---

## 技能系统

技能是一个包含 `SKILL.md` 文件的目录，文件需有 YAML front-matter 头部：

```
skills/
└── my-skill/
    └── SKILL.md
```

```markdown
---
name: my-skill
description: 为 Agent 提供某种有用的能力。
---

# My Skill

此处写 Agent 的操作说明……
```

技能按以下顺序从三个目录加载并合并：

| 目录 | 作用范围 |
|---|---|
| `~/.agents/skills/` | 系统级（跨工具共享） |
| `~/.blades/skills/` | blades 全局技能 |
| `<workspace>/skills/`（默认：`~/.blades/workspace/skills/`） | 当前工作空间本地技能 |

在对话中输入 `/reload`（或重启），Agent 会自动发现并使用新技能。

`blades init` 安装的内置技能：

| 技能 | 说明 |
|---|---|
| `blades-cron` | 安装在 `~/.blades/skills/` 下的全局技能，用于在对话中调度 Shell 命令或 Agent 对话任务 |

---

## MCP（模型上下文协议）

blades 支持连接外部 MCP 工具服务器。服务器配置使用与 Claude Desktop 相同的 `mcp.json` 格式，现有配置可直接复用。

MCP 服务器从两个文件加载并合并：

| 文件 | 作用范围 |
|---|---|
| `~/.blades/mcp.json` | 全局（所有工作空间） |
| `<workspace>/mcp.json`（默认：`~/.blades/workspace/mcp.json`） | 当前工作空间 |

也可在 `config.yaml` 的 `mcp:` 字段中以内联方式声明服务器。

### mcp.json 格式

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

省略 `transport` 时默认为 `stdio`。字符串值支持 `${ENV_VAR}` 环境变量展开。

---

## 记忆架构

默认情况下，blades 只会把 `AGENTS.md` 注入初始系统提示词。`AGENTS.md` 会指示 Agent 在运行时按需读取 SOUL.md、USER.md、MEMORY.md、最近日志以及 knowledges/。

| 层级 | 位置 | 说明 |
|---|---|---|
| L1 | `workspace/MEMORY.md` | 手动维护或提炼的长期记忆，主会话中读取 |
| L2 | `workspace/memory/YYYY-MM-DD.md` | 仅追加的每日对话日志 |
| L3 | `workspace/knowledges/*.md` | 按需加载的参考文件 |

---

## 许可证

Apache 2.0 — 详见 [LICENSE](../../LICENSE)。
