module github.com/codex-k8s/kodex

go 1.25.7

toolchain go1.25.8

require (
	github.com/caarlos0/env/v11 v11.3.1
	github.com/codex-k8s/kodex/libs/go/eventlog v0.0.0
	github.com/codex-k8s/kodex/libs/go/grpcserver v0.0.0
	github.com/codex-k8s/kodex/libs/go/migrationtest v0.0.0
	github.com/codex-k8s/kodex/libs/go/outbox v0.0.0
	github.com/codex-k8s/kodex/libs/go/postgres v0.0.0
	github.com/codex-k8s/kodex/libs/go/servicemain v0.0.0
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.9.2
	github.com/prometheus/client_golang v1.23.2
	go.yaml.in/yaml/v2 v2.4.3
	google.golang.org/grpc v1.79.3
	google.golang.org/protobuf v1.36.11
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/klauspost/compress v1.18.2 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.66.1 // indirect
	github.com/prometheus/procfs v0.16.1 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.67.0 // indirect
	go.opentelemetry.io/otel v1.42.0 // indirect
	go.opentelemetry.io/otel/metric v1.42.0 // indirect
	go.opentelemetry.io/otel/trace v1.42.0 // indirect
	golang.org/x/net v0.52.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/text v0.35.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260406210006-6f92a3bedf2d // indirect
)

replace github.com/codex-k8s/kodex/libs/go/eventlog => ./libs/go/eventlog

replace github.com/codex-k8s/kodex/libs/go/grpcserver => ./libs/go/grpcserver

replace github.com/codex-k8s/kodex/libs/go/migrationtest => ./libs/go/migrationtest

replace github.com/codex-k8s/kodex/libs/go/outbox => ./libs/go/outbox

replace github.com/codex-k8s/kodex/libs/go/postgres => ./libs/go/postgres

replace github.com/codex-k8s/kodex/libs/go/servicemain => ./libs/go/servicemain
