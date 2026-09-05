module github.com/codex-k8s/kodex/services/internal/email-bridge

go 1.26.6

require (
	github.com/caarlos0/env/v11 v11.4.1
	github.com/codex-k8s/kodex/libs/go/emailbridgeapi v0.0.0
	github.com/codex-k8s/kodex/libs/go/httpserver v0.0.0-00010101000000-000000000000
	github.com/codex-k8s/kodex/libs/go/observability v0.0.0-00010101000000-000000000000
	github.com/codex-k8s/kodex/libs/go/securefile v0.0.0
	github.com/codex-k8s/kodex/libs/go/serviceruntime v0.0.0-00010101000000-000000000000
	github.com/emersion/go-imap/v2 v2.0.0-beta.8
	github.com/emersion/go-sasl v0.0.0-20241020182733-b788ff22d5a6
	github.com/emersion/go-smtp v0.25.0
	github.com/jackc/pgx/v5 v5.8.0
	github.com/knadh/go-pop3 v1.0.2
	github.com/pressly/goose/v3 v3.26.0
	github.com/prometheus/client_golang v1.24.1
	golang.org/x/net v0.57.0
)

require github.com/codex-k8s/kodex/libs/go/internalrpcauth v0.0.0 // indirect

require (
	github.com/apapsch/go-jsonmerge/v2 v2.0.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/codex-k8s/kodex/libs/go/controlplaneapi v0.0.0
	github.com/codex-k8s/kodex/libs/go/controlplaneclient v0.0.0
	github.com/codex-k8s/kodex/libs/go/grpcserver v0.0.0 // indirect
	github.com/emersion/go-message v0.18.2 // indirect
	github.com/getsentry/sentry-go v0.48.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.29.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/mfridman/interpolate v0.0.2 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/oapi-codegen/runtime v1.7.0 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.70.1 // indirect
	github.com/prometheus/procfs v0.21.1 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.3 // indirect
	github.com/sethvargo/go-retry v0.3.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel v1.44.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.44.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.44.0 // indirect
	go.opentelemetry.io/otel/metric v1.44.0 // indirect
	go.opentelemetry.io/otel/sdk v1.44.0 // indirect
	go.opentelemetry.io/otel/trace v1.44.0 // indirect
	go.opentelemetry.io/proto/otlp v1.10.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260526163538-3dc84a4a5aaa // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
	google.golang.org/grpc v1.82.1
	google.golang.org/protobuf v1.36.12-0.20260120151049-f2248ac996af
)

replace github.com/codex-k8s/kodex/libs/go/emailbridgeapi => ../../../libs/go/emailbridgeapi

replace github.com/codex-k8s/kodex/libs/go/securefile => ../../../libs/go/securefile

replace github.com/codex-k8s/kodex/libs/go/httpserver => ../../../libs/go/httpserver

replace github.com/codex-k8s/kodex/libs/go/serviceruntime => ../../../libs/go/serviceruntime

replace github.com/codex-k8s/kodex/libs/go/observability => ../../../libs/go/observability

replace github.com/codex-k8s/kodex/libs/go/grpcserver => ../../../libs/go/grpcserver

replace github.com/codex-k8s/kodex/libs/go/internalrpcauth => ../../../libs/go/internalrpcauth

replace github.com/codex-k8s/kodex/libs/go/controlplaneapi => ../../../libs/go/controlplaneapi

replace github.com/codex-k8s/kodex/libs/go/controlplaneclient => ../../../libs/go/controlplaneclient
