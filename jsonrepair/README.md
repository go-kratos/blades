# JSON Repair

Package `jsonrepair` provides provider-neutral, source-preserving recovery for
malformed JSON emitted in model tool calls. The implementation uses only the Go
standard library.

```go
result, err := jsonrepair.Repair(toolInput)
if err != nil {
    return err
}
toolInput = result.JSON
```

Valid JSON is returned byte-for-byte unchanged. A successful malformed-input
repair preserves every received byte in order and reports the inserted syntax
in `Result.Edits`. The conservative engine can close truncated strings and
containers, escape quotation marks that cannot close their current string, and
insert unambiguous missing separators.

Repair returns an error for ambiguous input or syntax that requires deleting
or replacing source bytes, including comments, trailing commas, single-quoted
strings, and raw control characters. It cannot reconstruct content that the
model or transport never sent. Callers must still validate tool schemas and
apply their normal policy and authorization checks before execution.

Use `New` with `WithLimits` to configure input, output, nesting, and edit
limits. An `Engine` is immutable after construction and safe for concurrent
use.

## Provider Integration

Anthropic streaming, OpenAI Chat, and OpenAI Responses enable the default
repairer at their raw tool-argument boundaries. Applications can retain strict
rejection with `anthropic.WithToolInputJSONRepairer(nil)`,
`openai.WithToolInputJSONRepairer(nil)`, or
`openai.WithResponsesToolInputJSONRepairer(nil)`. Each provider accepts a
custom `Repairer` for limits and testing.

Gemini is not wired to this package because the Google SDK exposes only an
already-decoded argument map. MCP and OTel do not decode model responses. See
the [contrib integration matrix](../contrib) for the boundary rationale.

`testdata/` contains sanitized SSE reproductions of the three original failure
classes. The fixtures retain only synthetic tool-call events and safe argument
text; raw storage exports and their metadata are not retained.
