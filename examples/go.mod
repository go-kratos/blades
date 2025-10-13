module github.com/go-kratos/blades/examples

go 1.24.6

replace (
	github.com/go-kratos/blades => ../
	github.com/go-kratos/blades/contrib/openai => ../contrib/openai
)

require (
	github.com/go-kratos/blades v0.0.0-20250928061855-93360cba17ff
	github.com/go-kratos/blades/contrib/openai v0.0.0-00010101000000-000000000000
	github.com/google/jsonschema-go v0.3.0
)

require (
	github.com/go-kratos/generics v0.0.0-20251013122419-613f49d67c64 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/openai/openai-go/v2 v2.7.0 // indirect
	github.com/tidwall/gjson v1.18.0 // indirect
	github.com/tidwall/match v1.2.0 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
	golang.org/x/sync v0.16.0 // indirect
)
