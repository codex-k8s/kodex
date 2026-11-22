module github.com/codex-k8s/kodex/libs/go/managedkubernetes

go 1.26.10

require (
	github.com/codex-k8s/kodex/libs/go/internalrpcauth v0.0.0
	github.com/google/uuid v1.6.0
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.3
	k8s.io/apimachinery v0.36.3
)

replace github.com/codex-k8s/kodex/libs/go/internalrpcauth => ../internalrpcauth

require (
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.4.1 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/goccy/go-json v0.10.6 // indirect
	github.com/gowebpki/jcs v1.0.1 // indirect
	github.com/lestrrat-go/blackmagic v1.0.4 // indirect
	github.com/lestrrat-go/dsig v1.3.0 // indirect
	github.com/lestrrat-go/dsig-secp256k1 v1.0.0 // indirect
	github.com/lestrrat-go/httpcc v1.0.1 // indirect
	github.com/lestrrat-go/httprc/v3 v3.0.6 // indirect
	github.com/lestrrat-go/jwx/v3 v3.2.0 // indirect
	github.com/lestrrat-go/option/v2 v2.0.0 // indirect
	github.com/segmentio/asm v1.2.1 // indirect
	github.com/valyala/fastjson v1.6.10 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.42.0 // indirect
	k8s.io/klog/v2 v2.140.0 // indirect
	k8s.io/utils v0.0.0-20260210185600-b8788abfbbc2 // indirect
)
