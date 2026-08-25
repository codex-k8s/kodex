module github.com/codex-k8s/kodex/libs/go/oidcverifier

go 1.26.6

require (
	github.com/codex-k8s/kodex/libs/go/oidcidentity v0.0.0
	github.com/coreos/go-oidc/v3 v3.20.0
	github.com/go-jose/go-jose/v4 v4.1.4
	github.com/google/uuid v1.6.0
)

require golang.org/x/oauth2 v0.36.0 // indirect

replace github.com/codex-k8s/kodex/libs/go/oidcidentity => ../oidcidentity
