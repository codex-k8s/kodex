module github.com/codex-k8s/kodex/libs/go/dnsresolver

go 1.26.6

require (
	github.com/codex-k8s/kodex/libs/go/mailpolicy v0.0.0
	github.com/miekg/dns v1.1.72
)

require (
	github.com/apapsch/go-jsonmerge/v2 v2.0.0 // indirect
	github.com/codex-k8s/kodex/libs/go/emailbridgeapi v0.0.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/oapi-codegen/runtime v1.7.0 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.3 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/mod v0.37.0 // indirect
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	golang.org/x/tools v0.47.0 // indirect
)

replace github.com/codex-k8s/kodex/libs/go/mailpolicy => ../mailpolicy

replace github.com/codex-k8s/kodex/libs/go/emailbridgeapi => ../emailbridgeapi
