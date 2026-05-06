---
name: issue-triage
description: Triage newly opened Blades GitHub issues by applying existing repository labels without posting comments.
---

# Issue Triage

Use this skill for GitHub Actions issue triage in the Blades repository.

## Goal

Apply accurate existing labels to a newly opened issue. Do not comment on the issue. Do not create labels.

## Inputs

- Repository: provided by the workflow prompt.
- Issue number: provided by the workflow prompt.
- GitHub labels: fetch live from the repository before deciding.

## Workflow

1. Fetch the issue title, body, current labels, and comments.
2. Fetch the repository label list.
3. Search recent and open issues for similar reports only to improve routing context.
4. Infer issue type from the template, title prefix, body headings, and user intent.
5. Apply only labels that already exist in the repository.

## Blades Labeling Rules

- Preserve labels already added by issue templates unless they are clearly wrong.
- Prefer these template labels when present and available:
  - `bug` for broken behavior, regressions, panics, races, build failures, test failures, or incorrect output.
  - `feature` for new capability requests before implementation design is settled.
  - `proposal` for implementation-design issues with API shape, usage examples, or architecture details.
  - `question` for usage help, support questions, or clarification requests.
- If a report is incomplete but actionable labels are available, add the issue-type label and any relevant status label such as `needs-info`.
- If labels for package areas exist, use the affected surface:
  - root framework: `agent.go`, `runner.go`, `message.go`, `model.go`, `tool.go`, `state.go`, `session.go`.
  - `flow`, `graph`, `recipe`, `skills`, `memory`, `middleware`, `tools`, `stream`, `evaluator`.
  - provider integrations under `contrib/openai`, `contrib/anthropic`, `contrib/gemini`, `contrib/mcp`, `contrib/otel`.
  - CLI and runtime under `cmd/blades`.
- If no matching area labels exist, apply only the type/status labels.

## Evidence Rules

- Use the user's reported behavior as primary evidence. Treat guesses about root cause as hypotheses.
- Do not ask for private API keys, account identifiers, tokens, or full unredacted logs.
- For bugs, look for Blades version, Go version, OS, reproduction steps, expected behavior, and actual behavior.
- For feature requests and proposals, distinguish desired user capability from suggested implementation details.
- For questions, prefer `question` over `bug` unless the user provides a clear failure.

## Duplicate Handling

This skill may add a duplicate label only when another open issue is clearly the same problem. Do not comment; the separate `issue-deduplication` skill handles duplicate comments.

## Output

Apply labels through GitHub tools. Do not post a public explanation. If no label can be selected confidently, leave the issue unchanged.
