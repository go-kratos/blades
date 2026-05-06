---
name: issue-triage
description: Triage newly opened GitHub issues by applying existing repository labels without posting comments.
---

# Issue Triage

Use this skill for GitHub issue triage in any repository.

## Goal

Apply accurate existing labels to a newly opened issue. Do not comment on the issue. Do not create labels.

## Inputs

- Repository: provided by the workflow prompt.
- Issue number: provided by the workflow prompt.
- GitHub labels: fetch live with `gh label list --limit 200` before deciding.

## Workflow

1. Fetch the issue title, body, current labels, and comments.
2. Fetch the repository label list with `gh label list --limit 200`.
3. Search recent and open issues for similar reports only to improve routing context.
4. Infer issue type from the template, title prefix, body headings, and user intent.
5. Apply only labels that exist in the live repository label list.

## Labeling Rules

- Preserve labels already added by issue templates unless they are clearly wrong.
- Treat the live repository label list as the source of truth. Do not invent labels and do not assume common labels exist.
- Use these common label mappings only when the exact label exists in the live list:
  - `bug` for broken behavior, regressions, panics, races, build failures, test failures, or incorrect output.
  - `documentation` for docs, README, example, comment, or generated documentation issues.
  - `enhancement` for feature requests or proposals, including API shape, usage examples, or architecture ideas.
  - `question` for usage help, support questions, or clarification requests.
  - `invalid` for reports that are clearly not actionable, not related to the repository, or based on a false premise.
  - `duplicate` only when another open issue is clearly the same problem.
- If a report is incomplete, apply the best existing type label when confident. Add a status label only if the live list contains a clearly matching one.
- Use the affected surface as routing evidence, and apply area or component labels only when exact matching labels exist in the live list:
  - File paths, package names, modules, directories, commands, products, or integrations explicitly mentioned in the issue.
  - Template fields, issue title prefixes, stack traces, reproduction steps, or linked code references that identify a component.
  - Existing labels whose wording clearly matches the reported affected surface.
- If no matching area labels exist, apply only the available type/status labels.

## Evidence Rules

- Use the user's reported behavior as primary evidence. Treat guesses about root cause as hypotheses.
- Do not ask for private API keys, account identifiers, tokens, or full unredacted logs.
- For bugs, look for project version, dependency/runtime versions, OS or environment, reproduction steps, expected behavior, and actual behavior.
- For feature requests and proposals, distinguish desired user capability from suggested implementation details.
- For questions, prefer `question` over `bug` unless the user provides a clear failure.

## Duplicate Handling

This skill may add `duplicate` only when the label exists in the live list and another open issue is clearly the same problem. Do not comment; the separate `issue-deduplication` skill handles duplicate comments.

## Output

Apply labels through GitHub tools. Do not post a public explanation. If no label can be selected confidently, leave the issue unchanged.
