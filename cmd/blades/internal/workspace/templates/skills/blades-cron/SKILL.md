---
name: blades-cron
description: Schedule one-shot or recurring tasks - run a shell command or send a message to the agent on a timer.
---

# Cron

Use the `cron` tool to schedule tasks that run automatically.

## Actions

| Action | Purpose |
|---|---|
| `add` | Create a new scheduled job |
| `list` | Show all jobs with next run time and last status |
| `remove` | Delete a job by `job_id` |
| `run` | Execute a job immediately regardless of schedule |

---

## Schedule kinds

| Goal | `schedule_kind` | Required fields |
|---|---|---|
| One-shot in N seconds | *(omit)* | `delay_seconds: N` |
| One-shot at exact time | `at` | `at_ms: <unix ms>` |
| Repeat every N seconds | `every` | `every_seconds: N` |
| Standard cron expression | `cron` | `cron_expr: "min hr dom mon dow"` |

**Rules:**
- For "in N minutes/hours" use `delay_seconds` - never call `exec` to get the current timestamp, just pass the number of seconds.
- Use `delete_after_run: true` for one-shot jobs that should be removed after firing.
- For `schedule_kind=cron` use a standard 5-field expression (`min hour dom month dow`). Optional `tz` sets the timezone (IANA name, e.g. `"Asia/Shanghai"`).
- Valid `schedule_kind` values: `at`, `every`, `cron`. Do **not** use `delay` or other values.

---

## Payload kinds

| Goal | `payload_kind` | Required field |
|---|---|---|
| Run a shell command | `exec` | `command: "..."` |
| Inject a message into the agent | `agent_turn` | `message: "..."` |

**Rules:**
- `payload_kind` may be omitted: defaults to `exec` when `command` is set, otherwise `agent_turn`.
- Valid `payload_kind` values: `exec`, `agent_turn`. Do **not** use `message`, `agent_message`, or other values.
- `message` is a **field** containing the text to inject, not a `payload_kind` value.
- For `agent_turn` jobs, the agent's reply is stored in the job's `lastOutput` (visible via `action=list`).
- An optional `session_id` scopes an `agent_turn` job to a specific conversation.
- **`reply_session_id`** (optional): when set (e.g. to the current chat/session ID in Feishu), the job's output or agent reply is sent to that session instead of only being logged. Use this when adding cron jobs from a channel (Lark, etc.) so results are delivered back to the same chat.

---

## Examples

**One-shot in 10 minutes (exec):**
```
cron(action="add", name="ls home", delay_seconds=600,
     command="ls ~", delete_after_run=true)
```

**Repeat every hour (exec):**
```
cron(action="add", name="disk check", schedule_kind="every", every_seconds=3600,
     payload_kind="exec", command="df -h")
```

**Daily at 08:00 Shanghai time (agent_turn):**
```
cron(action="add", name="morning brief", schedule_kind="cron",
     cron_expr="0 8 * * *", tz="Asia/Shanghai",
     payload_kind="agent_turn",
     message="Summarise my pending tasks")
```

**Weekdays at 09:00 UTC (agent_turn):**
```
cron(action="add", name="standup", schedule_kind="cron",
     cron_expr="0 9 * * 1-5",
     payload_kind="agent_turn", message="Generate today's standup notes")
```

**List all jobs:**
```
cron(action="list")
```

**Remove a job:**
```
cron(action="remove", job_id="<id>")
```

**Run a job immediately:**
```
cron(action="run", job_id="<id>")
```