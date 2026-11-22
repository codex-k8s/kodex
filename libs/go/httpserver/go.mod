module github.com/codex-k8s/kodex/libs/go/httpserver

go 1.26.10

require (
	github.com/codex-k8s/kodex/libs/go/serviceruntime v0.0.0
	golang.org/x/net v0.57.0
)

replace github.com/codex-k8s/kodex/libs/go/serviceruntime => ../serviceruntime
