module github.com/go-kratos/blades/contrib/otel

go 1.24.0

toolchain go1.24.6

require (
	github.com/go-kratos/blades v0.0.0
	go.opentelemetry.io/otel v1.38.0
	go.opentelemetry.io/otel/trace v1.38.0
)

require (
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/google/jsonschema-go v0.3.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	go.opentelemetry.io/auto/sdk v1.1.0 // indirect
	go.opentelemetry.io/otel/metric v1.38.0 // indirect
	golang.org/x/sync v0.16.0 // indirect
)

replace github.com/go-kratos/blades => ../..
