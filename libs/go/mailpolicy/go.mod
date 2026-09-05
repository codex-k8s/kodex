module github.com/codex-k8s/kodex/libs/go/mailpolicy

go 1.26.6

require github.com/codex-k8s/kodex/libs/go/emailbridgeapi v0.0.0

require (
	github.com/apapsch/go-jsonmerge/v2 v2.0.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/oapi-codegen/runtime v1.7.0 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.3 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/text v0.32.0 // indirect
)

replace github.com/codex-k8s/kodex/libs/go/emailbridgeapi => ../emailbridgeapi
