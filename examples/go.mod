module github.com/go-kratos/blades/examples

go 1.24.6

replace (
	github.com/go-kratos/blades => ../
	github.com/go-kratos/blades/contrib/claude => ../contrib/claude
	github.com/go-kratos/blades/contrib/gemini => ../contrib/gemini
	github.com/go-kratos/blades/contrib/openai => ../contrib/openai
	github.com/go-kratos/blades/contrib/s3 => ../contrib/s3
)

require (
	github.com/go-kratos/blades v0.0.0-20250928061855-93360cba17ff
	github.com/go-kratos/blades/contrib/gemini v0.0.0-00010101000000-000000000000
	github.com/go-kratos/blades/contrib/openai v0.0.0-00010101000000-000000000000
	github.com/go-kratos/blades/contrib/s3 v0.0.0-00010101000000-000000000000
	github.com/google/jsonschema-go v0.3.0
	google.golang.org/genai v1.26.0
)

require (
	cloud.google.com/go v0.116.0 // indirect
	cloud.google.com/go/auth v0.9.3 // indirect
	cloud.google.com/go/compute/metadata v0.8.4 // indirect
	github.com/aws/aws-sdk-go-v2 v1.39.3 // indirect
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.7.2 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.4.10 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.7.10 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.4.10 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.2 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.9.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.13.10 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.19.10 // indirect
	github.com/aws/aws-sdk-go-v2/service/s3 v1.88.5 // indirect
	github.com/aws/smithy-go v1.23.1 // indirect
	github.com/go-kratos/generics v0.0.0-20251015114009-68dee470a252 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/s2a-go v0.1.9 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.6 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/openai/openai-go/v2 v2.7.0 // indirect
	github.com/tidwall/gjson v1.18.0 // indirect
	github.com/tidwall/match v1.2.0 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
	go.opencensus.io v0.24.0 // indirect
	golang.org/x/crypto v0.42.0 // indirect
	golang.org/x/net v0.44.0 // indirect
	golang.org/x/sync v0.17.0 // indirect
	golang.org/x/sys v0.36.0 // indirect
	golang.org/x/text v0.29.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250908214217-97024824d090 // indirect
	google.golang.org/grpc v1.75.1 // indirect
	google.golang.org/protobuf v1.36.9 // indirect
)
