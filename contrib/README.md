# Contrib Integrations

Tool-call JSON repair is installed at contrib boundaries that receive raw model
argument bytes:

| Package | Tool-input representation | Repair behavior |
| --- | --- | --- |
| `anthropic` | Accumulated `input_json_delta` bytes at block stop or clean EOF | Enabled by default; disable with `WithToolInputJSONRepairer(nil)` |
| `openai` Chat | Completed function argument string | Enabled by default; disable with `WithToolInputJSONRepairer(nil)` |
| `openai` Responses | Completed function argument string | Enabled by default; disable with `WithResponsesToolInputJSONRepairer(nil)` |
| `gemini` | SDK-decoded `map[string]any` | Not applicable because raw invalid JSON does not reach the adapter |
| `mcp` | Already-emitted `content.ToolUse.Input` | Not applied at the execution boundary |
| `otel` | No tool-input decoding | Not applicable |

The default repairer is the provider-neutral
[`github.com/go-kratos/blades/jsonrepair`](../jsonrepair) package. It runs only
at a provider terminal boundary and before the call is emitted to core model
protocol consumers. Schema validation, policy, authorization, and tool
execution remain separate steps.
