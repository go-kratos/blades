# Blades MCP

The MCP integration exposes remote MCP tools through the Blades
`tools.Resolver` interface. It uses the official Go MCP SDK for transport,
discovery, and invocation.

MCP is a tool resolver and executor, not a model response decoder. It receives
`content.ToolUse.Input` only after a model provider has emitted the call, so it
keeps strict JSON decoding and does not perform receive-side JSON repair. Repair
belongs at a provider boundary where the original model argument bytes are
available, before tool policy and execution.

## Dynamic request metadata

Use `ClientConfig.SendingMiddleware` when outgoing MCP requests need dynamic
metadata such as a tenant, caller, trace, or audit identity. Middleware receives
the request context and MCP method, so it can target `tools/call` or any other
current or future MCP request without changing tool input schemas.

```go
client, err := mcp.NewClient(mcp.ClientConfig{
	Name:      "inventory",
	Transport: mcp.TransportHTTP,
	Endpoint:  "https://mcp.example.com",
	SendingMiddleware: []sdkmcp.Middleware{
		func(next sdkmcp.MethodHandler) sdkmcp.MethodHandler {
			return func(ctx context.Context, method string, request sdkmcp.Request) (sdkmcp.Result, error) {
				if method == "tools/call" {
					meta := maps.Clone(request.GetParams().GetMeta())
					if meta == nil {
						meta = make(map[string]any)
					}
					meta["com.example/caller"] = callerFromContext(ctx)
					request.GetParams().SetMeta(meta)
				}
				return next(ctx, method, request)
			}
		},
	},
})
```

The metadata is serialized in the protocol-level `_meta` field. It is not part
of model-generated tool arguments. Use a namespaced key to avoid collisions and
copy an existing metadata map before mutating it when middleware may share it.
