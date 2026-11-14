module github.com/codex-k8s/kodex/services/external/integration-gateway

go 1.26.10

require (
	github.com/caarlos0/env/v11 v11.3.1
	github.com/codex-k8s/kodex/libs/go/controlplaneapi v0.0.0
	github.com/codex-k8s/kodex/libs/go/controlplaneclient v0.0.0
	github.com/codex-k8s/kodex/libs/go/credentialfs v0.0.0
	github.com/codex-k8s/kodex/libs/go/httpserver v0.0.0
	github.com/codex-k8s/kodex/libs/go/integrationpackage v0.0.0
	github.com/codex-k8s/kodex/libs/go/observability v0.0.0
	github.com/codex-k8s/kodex/libs/go/repositorybundle v0.0.0
	github.com/codex-k8s/kodex/libs/go/secretbrokerapi v0.0.0
	github.com/codex-k8s/kodex/libs/go/serviceruntime v0.0.0
	github.com/codex-k8s/kodex/libs/go/sshcompose v0.0.0
	github.com/getkin/kin-openapi v0.149.0
	github.com/google/go-github/v74 v74.0.0
	github.com/google/uuid v1.6.0
	github.com/modelcontextprotocol/go-sdk v1.8.0
	github.com/prometheus/prometheus v0.314.0
	golang.org/x/crypto v0.57.0
)

require (
	github.com/apapsch/go-jsonmerge/v2 v2.0.0 // indirect
	github.com/dennwc/varint v1.0.0 // indirect
	github.com/go-openapi/jsonpointer v0.23.2 // indirect
	github.com/go-openapi/swag/jsonname v0.26.1 // indirect
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/grafana/regexp v0.0.0-20250905093917-f7b3be9d1853 // indirect
	github.com/oapi-codegen/runtime v1.7.0 // indirect
	github.com/oasdiff/yaml v0.1.1 // indirect
	github.com/oasdiff/yaml3 v0.0.14 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.3 // indirect
	github.com/segmentio/asm v1.2.1 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	golang.org/x/oauth2 v0.36.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/time v0.15.0 // indirect
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/codex-k8s/kodex/libs/go/emailbridgeapi v0.0.0
	github.com/codex-k8s/kodex/libs/go/grpcserver v0.0.0 // indirect
	github.com/codex-k8s/kodex/libs/go/internalrpcauth v0.0.0
	github.com/codex-k8s/kodex/libs/go/securefile v0.0.0
	github.com/getsentry/sentry-go v0.48.0 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/google/go-querystring v1.2.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.29.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/prometheus/client_golang v1.24.1
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.70.1 // indirect
	github.com/prometheus/procfs v0.21.1 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel v1.45.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.45.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.45.0 // indirect
	go.opentelemetry.io/otel/metric v1.45.0 // indirect
	go.opentelemetry.io/otel/sdk v1.45.0 // indirect
	go.opentelemetry.io/otel/trace v1.45.0 // indirect
	go.opentelemetry.io/proto/otlp v1.11.0 // indirect
	go.yaml.in/yaml/v2 v2.4.4 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/net v0.60.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.42.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260803160001-6ac0973c030d // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260803160001-6ac0973c030d // indirect
	google.golang.org/grpc v1.83.2
	google.golang.org/protobuf v1.36.12-0.20260120151049-f2248ac996af
)

replace github.com/codex-k8s/kodex/libs/go/controlplaneapi => ../../../libs/go/controlplaneapi

replace github.com/codex-k8s/kodex/libs/go/controlplaneclient => ../../../libs/go/controlplaneclient

replace github.com/codex-k8s/kodex/libs/go/credentialfs => ../../../libs/go/credentialfs

replace github.com/codex-k8s/kodex/libs/go/grpcserver => ../../../libs/go/grpcserver

replace github.com/codex-k8s/kodex/libs/go/httpserver => ../../../libs/go/httpserver

replace github.com/codex-k8s/kodex/libs/go/integrationpackage => ../../../libs/go/integrationpackage

replace github.com/codex-k8s/kodex/libs/go/internalrpcauth => ../../../libs/go/internalrpcauth

replace github.com/codex-k8s/kodex/libs/go/observability => ../../../libs/go/observability

replace github.com/codex-k8s/kodex/libs/go/serviceruntime => ../../../libs/go/serviceruntime

replace github.com/codex-k8s/kodex/libs/go/securefile => ../../../libs/go/securefile

replace github.com/codex-k8s/kodex/libs/go/emailbridgeapi => ../../../libs/go/emailbridgeapi

replace github.com/codex-k8s/kodex/libs/go/repositorybundle => ../../../libs/go/repositorybundle

replace github.com/codex-k8s/kodex/libs/go/sshcompose => ../../../libs/go/sshcompose

replace github.com/codex-k8s/kodex/libs/go/secretbrokerapi => ../../../libs/go/secretbrokerapi
