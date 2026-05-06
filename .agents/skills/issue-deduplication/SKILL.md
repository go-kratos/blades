---
name: issue-deduplication
description: Detect high-confidence duplicate Blades GitHub issues and link them to the canonical issue.
---

# Issue Deduplication

Use this skill for GitHub Actions duplicate detection on newly opened Blades issues.

## Goal

Find true duplicates, not merely related issues. Only mark an issue as duplicate when confidence is high.

## Workflow

1. Fetch the new issue title, body, labels, and comments.
2. Search open and recently closed issues using multiple focused queries from:
   - normalized title keywords,
   - package/module names,
   - error messages, panic text, or failing test names,
   - API names, provider names, CLI command names, or workflow names.
3. Compare the new issue to the strongest candidates.
4. If high confidence, add the existing `duplicate` label when available and post one concise comment linking the canonical issue.
5. If confidence is medium or low, do nothing.

## Normalization

- Ignore low-signal prefixes such as `[Bug]`, `[Feature]`, `[Proposal]`, `[Question]`, `Bug:`, `Feature:`, and `Blades:`.
- Treat Go version, OS, provider, and CLI flags as supporting evidence. They are duplicate blockers only when they materially change the failing behavior.
- Treat wording differences as irrelevant when the same API, command, package, error, and reproduction path are involved.
- Do not collapse separate Blades surfaces just because they share broad terms such as agent, model, provider, tool, recipe, graph, flow, memory, or skill.

## High-Confidence Duplicate Criteria

Mark as duplicate only when at least two strong signals match:

- Same failing API, command, package, provider, or workflow.
- Same panic, error message, failing test, or incorrect observable behavior.
- Same minimal reproduction path or equivalent code example.
- Same requested capability with no meaningful difference in scope.
- Existing maintainer discussion already identifies the canonical issue.

## Do Not Mark Duplicate

- Same area but different symptom.
- Same desired outcome but different public API shape or compatibility concern.
- Similar provider issue that affects different provider contracts.
- Feature request and proposal are linked in process but not semantically duplicate.
- Question that can be answered by documentation, unless it asks exactly the same unresolved question.

## Comment Format

When marking a duplicate, post a short comment:

```markdown
This appears to duplicate #<canonical>. Please follow that issue for updates.
```

If there are multiple canonical issues, link the best one and mention the others only if necessary.

## Output

If duplicate: apply the existing `duplicate` label when available and post the comment. If not duplicate: make no changes and do not comment.
