module github.com/codex-k8s/matter-codex/services/internal/runtime-controller

go 1.26.5

require (
	github.com/caarlos0/env/v11 v11.4.1
	github.com/codex-k8s/matter-codex/libs/go/controlplaneapi v0.0.0
	github.com/codex-k8s/matter-codex/libs/go/controlplaneclient v0.0.0-00010101000000-000000000000
	github.com/codex-k8s/matter-codex/libs/go/serviceruntime v0.0.0-00010101000000-000000000000
	github.com/google/uuid v1.6.0
	github.com/openai/openai-go/v3 v3.52.0
	google.golang.org/grpc v1.83.1
	google.golang.org/protobuf v1.36.12
)

require (
	github.com/codex-k8s/matter-codex/libs/go/internalrpcauth v0.0.0 // indirect
	github.com/tidwall/gjson v1.19.0 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
)

replace github.com/codex-k8s/matter-codex/libs/go/controlplaneapi => ../../../libs/go/controlplaneapi

replace github.com/codex-k8s/matter-codex/libs/go/controlplaneclient => ../../../libs/go/controlplaneclient

replace github.com/codex-k8s/matter-codex/libs/go/eventing => ../../../libs/go/eventing

replace github.com/codex-k8s/matter-codex/libs/go/grpcserver => ../../../libs/go/grpcserver

replace github.com/codex-k8s/matter-codex/libs/go/httpserver => ../../../libs/go/httpserver

replace github.com/codex-k8s/matter-codex/libs/go/internalrpcauth => ../../../libs/go/internalrpcauth

replace github.com/codex-k8s/matter-codex/libs/go/observability => ../../../libs/go/observability

replace github.com/codex-k8s/matter-codex/libs/go/runtimecontract => ../../../libs/go/runtimecontract

replace github.com/codex-k8s/matter-codex/libs/go/serviceruntime => ../../../libs/go/serviceruntime
