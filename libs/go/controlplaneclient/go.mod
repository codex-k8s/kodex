module github.com/codex-k8s/kodex/libs/go/controlplaneclient

go 1.26.6

require (
	github.com/codex-k8s/kodex/libs/go/controlplaneapi v0.0.0
	github.com/codex-k8s/kodex/libs/go/internalrpcauth v0.0.0
	github.com/google/uuid v1.6.0
	google.golang.org/grpc v1.82.1
)

require (
	golang.org/x/net v0.53.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260414002931-afd174a4e478 // indirect
	google.golang.org/protobuf v1.36.12-0.20260120151049-f2248ac996af // indirect
)

replace github.com/codex-k8s/kodex/libs/go/controlplaneapi => ../controlplaneapi

replace github.com/codex-k8s/kodex/libs/go/internalrpcauth => ../internalrpcauth
