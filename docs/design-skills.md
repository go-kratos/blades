# Progressive Skill Loading

## Goals

Skills contain task-specific operating rules, business workflows, and reference material. An Agent's persistent system prompt includes only each Skill's name and description. The model loads the complete content only after deciding that a Skill is relevant, reducing unrelated context and instruction interference.

The implementation follows the common `SKILL.md` directory convention:

```text
skills/
└── add-friend/
    ├── SKILL.md
    ├── references/
    ├── assets/
    └── scripts/
```

`SKILL.md` uses YAML frontmatter for metadata such as the name, description, and compatibility requirements. Its Markdown body contains the complete instructions. Skills can be loaded from a local directory, an `fs.FS`, or an application-specific database adapter.

## Runtime Flow

```mermaid
flowchart LR
    A["Create Agent"] --> B["Validate Skills"]
    B --> C["Inject names and descriptions into the system prompt"]
    C --> D["Model determines relevance"]
    D -->|"Relevant"| E["Call load_skill"]
    E --> F["Return complete instructions as a tool result"]
    F --> G["Disclose tools declared by allowed-tools"]
    G --> H["Use the instructions and tools in the next model call"]
    H -->|"Resource required"| J["Call load_skill_resource"]
    D -->|"Not relevant"| I["Respond directly"]
```

The Blades Agent Loop already appends tool calls and tool results to the session before starting the next model call. Skills therefore reuse the existing Tool mechanism without changing the model loop.

## Public API

- `skills.Skill`: the minimal interface for database or remote registry implementations.
- `skills.New`: creates an in-memory Skill.
- `skills.NewFromDir`: loads Skills from a local directory.
- `skills.NewFromEmbed`: loads Skills from an `fs.FS`.
- `blades.WithSkills`: attaches Skills to an Agent.
- `list_skills`: returns the catalog available to the current Agent.
- `load_skill`: returns one Skill's complete instructions, metadata, and resource index.
- `load_skill_resource`: reads an additional resource by path.

## Authorization Boundary

Skill content tells the model how to perform a task; it does not decide whether the task is permitted. The three built-in Skill tools pass through Blades Policy like every other Tool. The `allowed-tools` frontmatter field controls progressive disclosure: a matching business tool is omitted from model requests until that Skill has been loaded. It cannot add a Tool to the Agent or expand the Agent's permissions.

Disclosure state belongs to one `Agent.Run`. Concurrent runs never share mutable state. A new run restores a Skill only when a successful `load_skill` result for the same Skill definition is still present in the model history; therefore the tools visible to the model remain consistent with the instructions it can see. Each tool wave uses an immutable disclosure snapshot, so a model cannot call `load_skill` and a newly disclosed business Tool in the same parallel wave.

The built-in implementation allows files under `scripts/` to be inspected as resources but never executes them. Applications that require script execution should provide a separate sandboxed Tool protected by Policy.

## Current Scope

This stage implements progressive loading for Skill instructions and Tools declared by `allowed-tools`. Tools that are not declared by any Skill remain visible for the whole run. Skill loading changes visibility from the next model call. Across runs, disclosure is derived from the history actually supplied to the model rather than from shared process state.
