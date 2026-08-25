module github.com/codex-k8s/kodex/services/external/control-api-gateway

go 1.26.6

require (
	github.com/caarlos0/env/v11 v11.4.1
	github.com/coder/websocket v1.8.14
	github.com/codex-k8s/kodex/libs/go/controlplaneapi v0.0.0
	github.com/codex-k8s/kodex/libs/go/controlplaneclient v0.0.0
	github.com/codex-k8s/kodex/libs/go/i18n v0.0.0
	github.com/codex-k8s/kodex/libs/go/observability v0.0.0
	github.com/codex-k8s/kodex/libs/go/oidcverifier v0.0.0
	github.com/codex-k8s/kodex/libs/go/securefile v0.0.0
	github.com/codex-k8s/kodex/libs/go/serviceruntime v0.0.0
	github.com/google/uuid v1.6.0
	github.com/nats-io/nats.go v1.52.0
	github.com/oapi-codegen/runtime v1.6.0
	github.com/prometheus/client_golang v1.23.2
	google.golang.org/grpc v1.82.1
	google.golang.org/protobuf v1.36.12-0.20260120151049-f2248ac996af
)

require (
	github.com/apapsch/go-jsonmerge/v2 v2.0.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/codex-k8s/kodex/libs/go/grpcserver v0.0.0 // indirect
	github.com/codex-k8s/kodex/libs/go/internalrpcauth v0.0.0 // indirect
	github.com/codex-k8s/kodex/libs/go/oidcidentity v0.0.0 // indirect
	github.com/coreos/go-oidc/v3 v3.20.0 // indirect
	github.com/getsentry/sentry-go v0.48.0 // indirect
	github.com/go-jose/go-jose/v4 v4.1.4 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.29.0 // indirect
	github.com/klauspost/compress v1.18.5 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/nats-io/nkeys v0.4.15 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	github.com/nicksnyder/go-i18n/v2 v2.6.1 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.66.1 // indirect
	github.com/prometheus/procfs v0.16.1 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel v1.44.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.44.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.44.0 // indirect
	go.opentelemetry.io/otel/metric v1.44.0 // indirect
	go.opentelemetry.io/otel/sdk v1.44.0 // indirect
	go.opentelemetry.io/otel/trace v1.44.0 // indirect
	go.opentelemetry.io/proto/otlp v1.10.0 // indirect
	go.yaml.in/yaml/v2 v2.4.2 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/crypto v0.51.0 // indirect
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/oauth2 v0.36.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260526163538-3dc84a4a5aaa // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
)

replace github.com/codex-k8s/kodex/libs/go/controlplaneapi => ../../../libs/go/controlplaneapi

replace github.com/codex-k8s/kodex/libs/go/controlplaneclient => ../../../libs/go/controlplaneclient

replace github.com/codex-k8s/kodex/libs/go/internalrpcauth => ../../../libs/go/internalrpcauth

replace github.com/codex-k8s/kodex/libs/go/i18n => ../../../libs/go/i18n

replace github.com/codex-k8s/kodex/libs/go/oidcidentity => ../../../libs/go/oidcidentity

replace github.com/codex-k8s/kodex/libs/go/oidcverifier => ../../../libs/go/oidcverifier

replace github.com/codex-k8s/kodex/libs/go/observability => ../../../libs/go/observability

replace github.com/codex-k8s/kodex/libs/go/serviceruntime => ../../../libs/go/serviceruntime

replace github.com/codex-k8s/kodex/libs/go/securefile => ../../../libs/go/securefile

replace github.com/codex-k8s/kodex/libs/go/grpcserver => ../../../libs/go/grpcserver
