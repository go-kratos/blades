module github.com/go-kratos/blades/contrib/s3

go 1.24.6

require (
	github.com/aws/aws-sdk-go-v2 v1.39.3
	github.com/aws/aws-sdk-go-v2/service/s3 v1.88.5
	github.com/go-kratos/blades v0.0.0-20250928061855-93360cba17ff
)

require (
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.7.2 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.4.10 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.7.10 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.4.10 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.2 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.9.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.13.10 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.19.10 // indirect
	github.com/aws/smithy-go v1.23.1 // indirect
	github.com/go-kratos/generics v0.0.0-20251015114009-68dee470a252 // indirect
	github.com/google/jsonschema-go v0.2.3 // indirect
	github.com/google/uuid v1.6.0 // indirect
	golang.org/x/sync v0.16.0 // indirect
)

replace github.com/go-kratos/blades => ../../
