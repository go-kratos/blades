---
type: design
title: JSON Repair Package
date: 2026-08-03
status: implemented
related: [design-model-provider.md, design-tool-system.md]
tags: [json, provider, tool-call, recovery]
---

# JSON Repair Package

## 1. Context

Provider streams can end with malformed JSON in a tool-call argument. Three
exported sessions contained three different failures, now represented by
sanitized SSE fixtures under `jsonrepair/testdata/`:

1. a string and its containing object end at EOF;
2. an outer object terminator is missing; and
3. quotation marks inside a string are not escaped.

An early prototype delegated repair to a GPL-3.0 implementation whose
normalization behavior can change received content. Blades now uses an
independently authored, standard-library-only implementation with no runtime
dependency on third-party JSON repair code.

The reviewed public compatibility corpus remains useful as an external
behavior oracle. Its cases cover unfinished containers, mixed quotation styles,
unquoted tokens, Markdown wrappers, invalid bytes, Unicode punctuation,
malformed separators, and multiple documents. The implementation source and
test file are not copied into this repository.

## 2. Decision

The package exposes one repair strategy. `PermissiveEngine` is the sole engine,
`New` constructs it, and `Repair` uses its zero value. There is no conservative
mode and no separate permissive constructor.

The name `PermissiveEngine` keeps the recovery semantics visible: malformed
input is interpreted heuristically and may require replacement or deletion.
Provider adapters install this engine by default wherever they receive raw
tool-argument bytes. Passing a nil `Repairer` remains the strict opt-out.

## 3. Goals

1. Keep JSON recovery provider-neutral and usable through `jsonrepair.Repairer`.
2. Recover every malformed category in the reviewed compatibility corpus.
3. Preserve already-valid UTF-8 JSON byte-for-byte.
4. Return valid JSON or an error; never return unchecked partial output.
5. Report a complete rewrite when malformed input changes.
6. Bound input, output, nesting, and lookahead for untrusted model output.
7. Keep repair separate from schema validation, policy, authorization, and tool
   execution.

## 4. Non-goals

- Guarantee that recovery reconstructs the model's intent.
- Reconstruct content that the model or transport never sent.
- Preserve every malformed source byte.
- Infer schema fields or business values.
- Decide whether a repaired tool call is safe to execute.
- Track compatibility with unreviewed future corpus changes.

## 5. Repair Contract

### 5.1 Valid Input

If the input is valid UTF-8 JSON:

- the output is a non-aliasing, byte-for-byte copy;
- `Result.Edits` is nil;
- `Result.Changed()` is false; and
- repeated repair returns the same bytes unchanged.

### 5.2 Malformed Input

Malformed input is recovered into a Go value and serialized with
`encoding/json`. Recovery may:

- normalize quotation marks, literal spelling, or number syntax;
- remove Markdown, prose, comments, trailing separators, or junk tokens;
- discard invalid UTF-8 bytes;
- close containers and strings;
- resolve missing or mismatched separators;
- resolve duplicate object keys using the last recovered value; and
- combine multiple root documents into one JSON array.

A successful malformed repair contains one `EditRewriteDocument` from byte zero
through the original input length. Applying the edit reproduces the returned
JSON. Repaired output passes `encoding/json.Valid` and is idempotent.

This contract does not promise no data loss. Removing a trailing comma or an
invalid byte cannot satisfy an insertion-only contract. A truncated string may
also be syntactically recoverable while still missing semantic content.

### 5.3 Failure

The engine returns no partial JSON when a configured resource limit is reached
or an internal serialization invariant fails. Error text contains a category
and byte offset but never includes source content, which may contain credentials
or user data.

## 6. Package Boundary and API

The package belongs at `github.com/go-kratos/blades/jsonrepair`:

```text
jsonrepair/
    repair.go                public API, engine, and limits
    edit.go                  edit and error types
    permissive_parser.go     semantic recovery parser
    api_test.go              public API and benchmarks
    permissive_test.go       recovery, limit, invariant, and fuzz tests
    sse_fixture_test.go      sanitized session regressions
    testdata/                reviewed SSE fixtures
```

It is a sibling package in the provider-agnostic root module and adds no symbols
to the root `blades` package.

```go
package jsonrepair

type Repairer interface {
    Repair(input []byte) (Result, error)
}

// PermissiveEngine is immutable after construction and safe for concurrent use.
type PermissiveEngine struct {
    // unexported limits
}

type Option func(*PermissiveEngine)

func New(options ...Option) *PermissiveEngine
func Repair(input []byte) (Result, error)
func (e PermissiveEngine) Repair(input []byte) (Result, error)

type Func func(input []byte) (Result, error)
func (f Func) Repair(input []byte) (Result, error)

type Result struct {
    JSON  []byte
    Edits []Edit
}

func (r Result) Changed() bool

type Edit struct {
    Kind        EditKind
    Start       int
    End         int
    Replacement string
}

type EditKind string

const EditRewriteDocument EditKind = "rewrite_document"

type Limits struct {
    MaxInputBytes  int
    MaxOutputBytes int
    MaxDepth       int
}

func WithLimits(limits Limits) Option
```

The defaults are 1 MiB input, 1 MiB plus 64 KiB output, and depth 128. A
non-positive limit field retains its default value.

Errors support `errors.Is` for these categories:

```go
var (
    ErrLimitExceeded = errors.New("json repair limit exceeded")
    ErrInvalidOutput = errors.New("json repair produced invalid output")
)
```

## 7. Recovery Algorithm

### 7.1 Pipeline

1. Enforce the input and initial output bounds.
2. Return valid UTF-8 JSON unchanged.
3. Decode input into runes while retaining original byte offsets for errors.
4. Locate object or array roots and ignore surrounding prose or Markdown.
5. Parse objects, arrays, strings, bare tokens, literals, and numbers into Go
   values using a depth-limited recursive-descent parser.
6. Serialize the recovered value with `encoding/json`.
7. Enforce the final output bound and validate the serialization.
8. Return one whole-document rewrite edit.

### 7.2 Structural Normalization

The parser recognizes smart and full-width quotation marks as string
delimiters. Full-width braces, brackets, commas, and colons are interpreted as
JSON syntax only in structural parser states. The same punctuation remains
unchanged when it occurs inside a recovered string value.

This distinction matters for multilingual tool arguments. For example, the
full-width comma in a Chinese prompt is content, while the same rune between
two full-width object members is syntax.

### 7.3 Strings and Quotes

The parser tracks whether a string is an object key or value and uses container
context plus bounded lookahead to decide whether a quotation mark closes the
string. It supports:

- unescaped double quotes inside double-quoted values;
- single-quoted keys and values;
- repeated quotation marks around keys or values;
- missing closing quotation marks;
- invalid escapes by retaining the escaped character; and
- raw key newlines by omitting the invalid control character.

Single and double quotation marks are not interchangeable after a string has
opened. This preserves apostrophes inside double-quoted tool values, including
EOF-truncated shell commands.

### 7.4 Objects, Arrays, and Bare Tokens

Objects accept quoted or bare keys, missing colons or commas, trailing commas,
junk between members, and missing or mismatched terminators. Arrays accept
missing commas, trailing commas, invalid empty items, and a misplaced object
closer before the array terminator.

Bare values are mapped case-insensitively to `true`, `false`, and `null` where
applicable. Valid number syntax is retained, and leading-decimal forms such as
`.25` receive a zero prefix. Other bare values become strings.

### 7.5 Resource Behavior

The parser uses no general backtracking. Whitespace lookup is precomputed, and
ambiguous quote/key lookahead is capped at 4096 runes. Input, output, and depth
limits bound work on untrusted arguments. Final object serialization may sort
map keys through `encoding/json`.

## 8. Provider Integration

Repair is enabled by default at contrib boundaries that can access raw model
tool-argument bytes:

- Anthropic accumulated `input_json_delta` bytes at block stop or clean EOF;
- OpenAI Chat completed function arguments; and
- OpenAI Responses completed function arguments.

Each constructor installs `jsonrepair.New()`, whose concrete type is
`*jsonrepair.PermissiveEngine`. Passing nil preserves strict rejection:

```go
anthropic.NewModel(modelName,
    anthropic.WithToolInputJSONRepairer(nil),
)

openai.NewChat(modelName,
    openai.WithToolInputJSONRepairer(nil),
)

openai.NewResponses(modelName,
    openai.WithResponsesToolInputJSONRepairer(nil),
)
```

Provider options continue to accept any custom `jsonrepair.Repairer` for tests
or application-specific behavior.

Gemini receives `genai.FunctionCall.Args` as an already-decoded
`map[string]any`; malformed source JSON has already been accepted or rejected by
the Google SDK. MCP is a tool execution integration, and OTel is an observation
integration, so neither is a raw model-response repair boundary.

After repair, normal schema validation, policy checks, and authorization still
apply. JSON recovery does not make a tool invocation trusted.

## 9. Test and Compatibility Evidence

### 9.1 Repository Tests

Blades-owned tests cover:

- the three sanitized SSE failure classes;
- Markdown and prose wrappers;
- truncated strings, objects, and arrays;
- single, repeated, smart, and full-width quotes;
- bare keys, values, literals, and numbers;
- invalid escapes, control characters, and UTF-8 bytes;
- missing and mismatched separators or closers;
- multiple documents and duplicate keys;
- preservation of valid input and punctuation inside string values;
- input, output, and depth limits;
- byte-offset error reporting;
- edit replay and idempotence; and
- arbitrary byte input through fuzzing.

The raw CSV exports are not retained. The SSE fixtures contain only synthetic
tool-call events, safe argument text, and the minimum fields required to
reproduce each failure.

### 9.2 External Compatibility

An out-of-tree adapter runs `jsonrepair.Repair` against all 80 active cases in
a public compatibility corpus. All 80 produce semantically equal JSON values.
The GPL implementation, test file, and compatibility adapter remain outside
the Blades worktree.

Passing this reviewed corpus establishes compatibility with those cases. It
does not establish that every possible malformed byte sequence has one correct
repair.

## 10. Independent Implementation Rules

To keep the implementation independently authored:

- use RFC 8259 as the grammar source;
- use public behavior examples only to identify recovery categories;
- do not translate GPL source, comments, function structure, or test files;
- keep repository test inputs independently authored or derived from sanitized
  Blades sessions;
- keep external compatibility tests and adapters outside the worktree; and
- retain normal project copyright and license review before release.

These engineering rules reduce source-copying risk but are not a substitute
for legal advice or a formal clean-room process.

## 11. Implementation Status

The implemented package includes:

1. one standard-library-only `PermissiveEngine` behind `New` and `Repair`;
2. default-on Anthropic, OpenAI Chat, and OpenAI Responses integration;
3. a nil repairer as the strict provider opt-out;
4. sanitized SSE regression fixtures;
5. independently authored unit, invariant, limit, benchmark, and fuzz tests;
6. semantic compatibility with all 80 active cases in the reviewed external
   corpus; and
7. no third-party JSON repair dependency in any module graph.

## 12. References

- [RFC 8259: The JavaScript Object Notation Data Interchange Format](https://www.rfc-editor.org/rfc/rfc8259)
