module github.com/go-kratos/blades/examples

go 1.24.6

replace (
	github.com/go-kratos/blades => ../
	github.com/go-kratos/blades/contrib/anthropic => ../contrib/anthropic
	github.com/go-kratos/blades/contrib/gemini => ../contrib/gemini
	github.com/go-kratos/blades/contrib/mcp => ../contrib/mcp
	github.com/go-kratos/blades/contrib/openai => ../contrib/openai
	github.com/go-kratos/blades/contrib/otel => ../contrib/otel
)

require (
	github.com/go-kratos/blades v0.0.0
	github.com/go-kratos/blades/contrib/gemini v0.0.0-00010101000000-000000000000
	github.com/go-kratos/blades/contrib/mcp v0.0.0-20251106103709-242709515a73
	github.com/go-kratos/blades/contrib/openai v0.0.0-20251106103709-242709515a73
	github.com/go-kratos/blades/contrib/otel v0.0.0-20251106103709-242709515a73
	github.com/google/jsonschema-go v0.3.0
	go.opentelemetry.io/otel v1.38.0
	go.opentelemetry.io/otel/exporters/stdout/stdouttrace v1.38.0
	go.opentelemetry.io/otel/sdk v1.38.0
	google.golang.org/genai v1.34.0
)

require (
	cloud.google.com/go v0.123.0 // indirect
	cloud.google.com/go/auth v0.17.0 // indirect
	cloud.google.com/go/compute/metadata v0.9.0 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/go-kratos/kit v0.0.0-20251121083925-65298ad2aa44 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/s2a-go v0.1.9 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.7 // indirect
	github.com/googleapis/gax-go/v2 v2.15.0 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/modelcontextprotocol/go-sdk v1.1.0 // indirect
	github.com/openai/openai-go/v3 v3.8.1 // indirect
	github.com/tidwall/gjson v1.18.0 // indirect
	github.com/tidwall/match v1.2.0 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.63.0 // indirect
	go.opentelemetry.io/otel/metric v1.38.0 // indirect
	go.opentelemetry.io/otel/trace v1.38.0 // indirect
	golang.org/x/crypto v0.43.0 // indirect
	golang.org/x/net v0.46.0 // indirect
	golang.org/x/oauth2 v0.32.0 // indirect
	golang.org/x/sync v0.17.0 // indirect
	golang.org/x/sys v0.37.0 // indirect
	golang.org/x/text v0.30.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251103181224-f26f9409b101 // indirect
	google.golang.org/grpc v1.76.0 // indirect
	google.golang.org/protobuf v1.36.10 // indirect
)
