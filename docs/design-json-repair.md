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

The first prototype delegated repair to
`github.com/RealAlexandreAI/json-repair`. That dependency was GPL-3.0 and its
normalization behavior could change received string content. It has been
replaced by an implementation inside Blades with no external runtime
dependency, usable by every provider adapter.

The referenced project is useful as a public behavior survey. Its documented
examples include unfinished arrays and objects, mixed quotation styles,
unquoted tokens, comments, Markdown wrappers, and malformed separators. This
design does not translate its source code or internal structure. The
implementation requirements come from RFC 8259, independently written tests,
and the captured Blades failures.

## 2. Goals

1. Add a provider-neutral `jsonrepair/` package in the root Go module.
2. Repair the three observed failure classes without removing or replacing any
   received byte.
3. Return valid JSON or an error; never return an unchecked partial result.
4. Preserve already-valid JSON byte-for-byte.
5. Report every inserted byte so callers can audit whether and how the input
   changed.
6. Keep runtime and memory usage bounded for untrusted model output.
7. Keep repair separate from provider streaming, schema validation, policy, and
   tool execution.

## 3. Non-goals

- Reconstruct content that the model or transport never sent.
- Infer schema fields or business values.
- Decide whether a repaired tool call is safe to execute.
- Accept every malformed format documented by another repair library in v1.
- Normalize whitespace, punctuation, Unicode, number formatting, object key
  order, or duplicate keys.
- Extract JSON from prose or Markdown in the conservative implementation.

## 4. Preservation Contract

“No data loss” needs a narrower definition than “the model's original intent
was recovered.” The latter cannot be guaranteed for truncated or ambiguous
input.

The v1 contract is:

- If input is valid JSON, output is a byte-for-byte copy of the input and the
  edit list is empty.
- If repair succeeds, every input byte occurs in the output in the same order.
  V1 may insert bytes but may not delete or replace source bytes.
- Each insertion is recorded at a byte boundary in the original input.
- The repaired output passes `encoding/json.Valid` before it is returned.
- Calling `Repair` again on repaired output returns it unchanged.

This contract can close a string or container and can insert an escape before
an unescaped quotation mark. It cannot safely fix syntax that requires removal
or substitution, including comments, trailing commas, single-quoted strings,
invalid control characters inside strings, or case changes such as `TRUE` to
`true`. V1 returns an error for those inputs.

Closing a truncated string preserves all received bytes, but it does not prove
that the received value is complete. Callers must treat `EditCloseString` and
EOF container edits as recovery from truncation, not as evidence that no
content is missing.

## 5. Package Boundary

The package belongs at `github.com/go-kratos/blades/jsonrepair`:

```text
jsonrepair/
    repair.go       public API and defaults
    edit.go         edit and error types
    parser.go       tolerant recursive-descent parser
    string.go       JSON string and quotation handling
    repair_test.go  focused and property tests
    testdata/       small, reviewed malformed inputs
```

This location keeps it independent of Anthropic, OpenAI, Gemini, and any
provider SDK. It is a sibling package in the provider-agnostic root module; it
does not add symbols to the root `blades` package. The implementation should
use only the Go standard library.

## 6. API

```go
package jsonrepair

// Repairer repairs one complete sequence of received JSON bytes.
type Repairer interface {
    Repair(input []byte) (Result, error)
}

// Engine is an immutable repairer and is safe for concurrent use. Its zero
// value uses the default limits.
type Engine struct {
    // unexported limits and parser configuration
}

// Option configures an Engine during New.
type Option func(*Engine)

// New constructs a conservative repair engine.
func New(options ...Option) *Engine

// Repair uses the default conservative engine.
func Repair(input []byte) (Result, error)

// Repair repairs input with this engine.
func (e *Engine) Repair(input []byte) (Result, error)

// Func adapts a function to Repairer, primarily for provider injection and
// tests.
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

const (
    EditEscapeQuote  EditKind = "escape_quote"
    EditCloseString  EditKind = "close_string"
    EditCloseObject  EditKind = "close_object"
    EditCloseArray   EditKind = "close_array"
    EditInsertColon  EditKind = "insert_colon"
    EditInsertComma  EditKind = "insert_comma"
)

type Limits struct {
    MaxInputBytes  int
    MaxOutputBytes int
    MaxDepth       int
    MaxEdits       int
}

func WithLimits(limits Limits) Option
```

`Edit.Start` and `Edit.End` are offsets into the original byte slice. In the
conservative v1 implementation they are always equal, which identifies an
insertion boundary. Keeping both fields allows a future, explicitly permissive
API to report replacements without changing the result shape.

The defaults are 1 MiB input, 1 MiB plus 64 KiB output, depth 128, and
256 edits. A caller can lower these limits for a particular provider or tool.

Errors should support `errors.Is` for the following categories:

```go
var (
    ErrUnrepairable   = errors.New("json is not conservatively repairable")
    ErrAmbiguous      = errors.New("json repair is ambiguous")
    ErrLimitExceeded  = errors.New("json repair limit exceeded")
    ErrInvalidOutput  = errors.New("json repair produced invalid output")
)
```

A concrete error can additionally expose the original byte offset and parser
state for diagnostics. It should not include the full tool input in its error
text because arguments may contain credentials or user data.

## 7. Repair Algorithm

### 7.1 Pipeline

1. Enforce the input limit.
2. If `encoding/json.Valid(input)` is true, return a copy without edits.
3. Parse the original bytes with a tolerant recursive-descent parser while
   writing them unchanged to an output buffer.
4. Insert only grammar characters that are required by an unambiguous local
   recovery.
5. Enforce depth, edit, and output limits during parsing.
6. Validate the completed output with `encoding/json.Valid`.
7. Return the output and ordered edit list, or return an error without JSON.

The parser should operate directly on bytes and spans. It must not unmarshal
into `map[string]any` and marshal again, because doing so can change whitespace,
number spelling, escaped Unicode, key order, and duplicate keys.

### 7.2 Parser States

The parser tracks the same structural expectations as the JSON grammar:

- root value;
- object key or object end;
- colon;
- object value;
- object comma or object end;
- array value or array end; and
- array comma or array end.

The container stack is also the source of EOF repairs. At EOF, an open string
is closed first, followed by open arrays and objects in reverse order.

### 7.3 Unescaped Quotation Marks

When an unescaped `"` appears inside a string, the parser considers the
surrounding grammar state and bounded lookahead:

- A key string can close before a colon.
- A value string can close before a comma, matching container terminator, or
  EOF.
- A quote followed by content that cannot legally follow a closed string is
  treated as string content and receives an inserted backslash.
- If closing and escaping are both plausible, repair fails with
  `ErrAmbiguous` rather than choosing silently.

For the observed input:

```json
{"question":{"prompt":"输入"已登录"后继续，或取消。"}}
```

the quotation marks around `已登录` cannot terminate the value at those
positions, so the output becomes:

```json
{"question":{"prompt":"输入\"已登录\"后继续，或取消。"}}
```

No character in the prompt is removed or normalized.

### 7.4 Missing Separators

A colon or comma may be inserted only when the current state requires it and
the next token can begin the corresponding value or member unambiguously.
Inputs that admit multiple interpretations return `ErrAmbiguous`.

### 7.5 Unsupported Repairs

The conservative parser rejects repairs that require deleting or replacing
source bytes. A later permissive mode could support those cases, but it should
have a separate constructor or explicit mode and its result must report source
ranges that were replaced. It must not weaken the v1 preservation contract.

## 8. Provider Integration

Provider adapters accumulate their native JSON deltas and invoke the repairer
only after strict validation fails at a provider-defined terminal boundary.
The package itself has no streaming API and knows nothing about tool IDs, tool
names, stop reasons, or provider events.

Repair is enabled by default at every contrib boundary where the adapter can
access raw model tool-argument bytes: Anthropic streaming, OpenAI Chat, and
OpenAI Responses. Each constructor creates its own default immutable engine;
there is no mutable global repair configuration. Strict behavior remains an
explicit option:

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

Providers also accept a custom `jsonrepair.Repairer` for resource limits and
test doubles. Conceptually, each applicable provider configuration includes:

```go
type Config struct {
    ToolInputJSONRepairer jsonrepair.Repairer
}
```

Each constructor seeds this field with `jsonrepair.New()` before applying
options. A nil value after option application therefore unambiguously disables
repair, whether supplied through the repairer option or a full config. This
keeps repair out of `model.Option`: JSON repair is local receive-side recovery,
not a request hint sent to a model provider.

Gemini receives `genai.FunctionCall.Args` as an already-decoded
`map[string]any`; raw malformed JSON has either been accepted or rejected by
the Google SDK before the adapter runs. Re-marshaling that map and applying a
repairer would be a no-op and would not recover rejected source bytes. MCP is a
tool execution integration, and OTel is an observation integration, so neither
is a model response repair boundary.

Anthropic treats clean stream EOF as the terminal boundary for every
accumulated tool block, even when `content_block_stop` was not received. The
block passes through the same strict-validation-then-repair path as a normally
completed block. Disabling JSON repair still rejects malformed EOF input, and
an SDK or transport error remains an error rather than a repair boundary.

After repair, normal tool schema validation, policy checks, and authorization
still apply. The repair package does not make a tool invocation trusted.

## 9. Test Strategy

### 9.1 Required Examples

Focused tests cover:

- the three captured failure classes;
- nested combinations of truncated strings, arrays, and objects;
- quotes inside ASCII and non-ASCII string values;
- missing separators that have one valid interpretation;
- ambiguous quote and separator cases;
- unsupported comments, trailing commas, single quotes, and raw control
  characters;
- every configured limit; and
- inputs containing secrets to ensure errors do not echo source text.

The raw CSV exports are not retained. Small reviewed SSE fixtures reproduce
their syntax failures without storage metadata, traces, real tool-call IDs, or
original argument content. Fixture tests validate both the SSE framing and the
restricted event/data fields before exercising repair.

### 9.2 Invariants

Fuzz and property tests should verify:

1. valid input is byte-for-byte unchanged;
2. successful output is valid JSON;
3. input is a byte subsequence of every successfully repaired output;
4. every output change is represented by an edit;
5. applying the edit list to the input reproduces the output;
6. repair is deterministic and idempotent; and
7. arbitrary bytes never cause a panic or unbounded recursion.

### 9.3 Performance

The target is O(n) time and O(n + depth + edits) memory. Lookahead must be
bounded and the implementation must not use general backtracking. Benchmarks
should include a valid 1 MiB document, a deeply nested document at the depth
limit, and a quote-heavy invalid string.

## 10. Implementation Status

The implemented v1 includes:

1. a standard-library-only engine and edit-report API in `jsonrepair/`;
2. focused, fuzz-seed, limit, and source-preservation invariant tests;
3. sanitized SSE regression fixtures derived from the three exported sessions;
4. default-on Anthropic streaming repair with a nil repairer as the strict
   opt-out;
5. default-on OpenAI Chat and Responses repair for both completed streaming
   and non-streaming tool calls, with matching strict opt-outs;
6. custom repairer injection on each applicable provider;
7. automatic Anthropic tool-block finalization at clean stream EOF; and
8. no `github.com/RealAlexandreAI/json-repair` dependency in any module graph.

## 11. Independent Implementation Rules

To keep the implementation independently authored:

- use RFC 8259 as the grammar source;
- use the public feature descriptions of other projects only to identify
  problem categories;
- do not translate GPL source, comments, function structure, or test files;
- write Blades test cases from the captured failures and this document; and
- retain normal project copyright and license review before release.

These engineering rules reduce source-copying risk but are not a substitute
for legal advice or a formal clean-room process.

## 12. Future Review Decisions

V1 uses insertion-only repair, returns a full edit report, finalizes Anthropic
tool blocks at clean stream EOF, and retains only sanitized SSE fixtures.
Future review can still consider:

1. a separately named permissive mode for replacements or deletions;
2. a convenience API that returns only repaired bytes;
3. provider-specific default limits; and
4. adding more reviewed and sanitized session-derived fixtures.

## 13. References

- [RFC 8259: The JavaScript Object Notation Data Interchange Format](https://www.rfc-editor.org/rfc/rfc8259)
- [`RealAlexandreAI/json-repair` public README and API](https://pkg.go.dev/github.com/RealAlexandreAI/json-repair) (behavior survey only; GPL-3.0 implementation is not reused)
