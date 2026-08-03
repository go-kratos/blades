# JSON Repair

Package `jsonrepair` provides provider-neutral semantic recovery for malformed
JSON emitted in model tool calls. It uses only the Go standard library.

```go
result, err := jsonrepair.Repair(toolInput)
if err != nil {
    return err
}
toolInput = result.JSON
```

`PermissiveEngine` is the package's only engine. `New` constructs it, and
`Repair` uses its zero value:

```go
repairer := jsonrepair.New(
    jsonrepair.WithLimits(jsonrepair.Limits{
        MaxInputBytes:  256 << 10,
        MaxOutputBytes: 320 << 10,
        MaxDepth:       64,
    }),
)
```

Valid UTF-8 JSON is returned byte-for-byte unchanged. Malformed input is parsed
into a recovered value and serialized with `encoding/json`. The engine can
recover Markdown or prose wrappers, single or redundant quotes, unescaped
quotes inside values, bare keys and values, case-insensitive literals, invalid
escapes and UTF-8 bytes, smart or full-width syntax, trailing commas, missing
separators or terminators, mismatched closers, and multiple JSON documents.

A malformed repair can normalize or discard invalid syntax. Changed results
therefore contain one `EditRewriteDocument` over the complete input. This edit
reproduces the returned JSON, but it is not a no-data-loss guarantee. The
engine cannot prove the model's intended value or reconstruct content that was
never received.

All successful results are valid JSON and become unchanged on the next repair.
Callers must still validate tool schemas and apply their normal policy and
authorization checks before execution.

## Provider Integration

Anthropic streaming, OpenAI Chat, and OpenAI Responses install
`*jsonrepair.PermissiveEngine` by default at their raw tool-argument boundaries.
Passing nil disables receive-side repair:

```go
anthropic.WithToolInputJSONRepairer(nil)
openai.WithToolInputJSONRepairer(nil)
openai.WithResponsesToolInputJSONRepairer(nil)
```

Each option also accepts a custom `jsonrepair.Repairer`. Gemini is not wired to
this package because the Google SDK exposes only an already-decoded argument
map. MCP and OTel do not decode raw model responses. See the
[contrib integration matrix](../contrib) for the boundary rationale.

`testdata/` contains sanitized SSE reproductions of the three original failure
classes. The fixtures retain only synthetic tool-call events and safe argument
text; raw storage exports and their metadata are not retained.
