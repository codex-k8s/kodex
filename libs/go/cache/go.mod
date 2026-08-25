module github.com/codex-k8s/kodex/libs/go/cache

go 1.26.6

require (
	github.com/codex-k8s/kodex/libs/go/securefile v0.0.0
	github.com/redis/go-redis/v9 v9.21.0
)

replace github.com/codex-k8s/kodex/libs/go/securefile => ../securefile

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
)
