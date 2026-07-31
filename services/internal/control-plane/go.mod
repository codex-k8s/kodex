module github.com/codex-k8s/matter-codex/services/internal/control-plane

go 1.26.5

require (
	github.com/codex-k8s/matter-codex/libs/go/cache v0.0.0-00010101000000-000000000000
	github.com/codex-k8s/matter-codex/libs/go/eventing v0.0.0
	github.com/codex-k8s/matter-codex/libs/go/internalrpcauth v0.0.0-00010101000000-000000000000
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.10.0
	github.com/robfig/cron/v3 v3.0.1
	google.golang.org/grpc v1.82.1
	google.golang.org/protobuf v1.36.12-0.20260120151049-f2248ac996af
)

require (
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.4.1 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/goccy/go-json v0.10.6 // indirect
	github.com/gowebpki/jcs v1.0.1 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/lestrrat-go/blackmagic v1.0.4 // indirect
	github.com/lestrrat-go/dsig v1.3.0 // indirect
	github.com/lestrrat-go/dsig-secp256k1 v1.0.0 // indirect
	github.com/lestrrat-go/httpcc v1.0.1 // indirect
	github.com/lestrrat-go/httprc/v3 v3.0.6 // indirect
	github.com/lestrrat-go/jwx/v3 v3.2.0 // indirect
	github.com/lestrrat-go/option/v2 v2.0.0 // indirect
	github.com/segmentio/asm v1.2.1 // indirect
	github.com/valyala/fastjson v1.6.10 // indirect
	go.opentelemetry.io/otel v1.44.0 // indirect
	go.opentelemetry.io/otel/sdk/metric v1.44.0 // indirect
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260720211330-0afa2a65878a // indirect
)

replace github.com/codex-k8s/matter-codex/libs/go/cache => ../../../libs/go/cache

replace github.com/codex-k8s/matter-codex/libs/go/eventing => ../../../libs/go/eventing

replace github.com/codex-k8s/matter-codex/libs/go/grpcserver => ../../../libs/go/grpcserver

replace github.com/codex-k8s/matter-codex/libs/go/internalrpcauth => ../../../libs/go/internalrpcauth

replace github.com/codex-k8s/matter-codex/libs/go/observability => ../../../libs/go/observability

replace github.com/codex-k8s/matter-codex/libs/go/serviceruntime => ../../../libs/go/serviceruntime
