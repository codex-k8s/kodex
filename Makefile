GOVULNCHECK_VERSION := v1.6.0
GO_MIN_VERSION := 1.26.5
GO_TOOLCHAIN := go1.26.5
BUF_VERSION := 1.71.0
PROTOBUF_GO_PLUGIN_REMOTE := buf.build/protocolbuffers/go:v1.36.11
PROTOBUF_GO_PLUGIN_REVISION := 1
GRPC_GO_PLUGIN_REMOTE := buf.build/grpc/go:v1.6.2
GRPC_GO_PLUGIN_REVISION := 1
ASYNCAPI ?= asyncapi
CONTROL_API_GATEWAY_ASYNCAPI_VERSION := @asyncapi/cli/6.0.2

.PHONY: check-go-toolchain check-proto-toolchain check-control-api-gateway-asyncapi-toolchain test-go-toolchain-contract test-go test-go-postgres test-go-all test-render-evidence tidy-go govulncheck gen-openapi gen-openapi-go gen-integration-gateway-openapi-go gen-interaction-gateway-openapi-go gen-control-api-gateway-openapi-go gen-control-api-gateway-asyncapi check-control-api-gateway-asyncapi-codegen lint-control-api-gateway-asyncapi gen-openapi-ts lint-proto build-proto gen-proto check-proto-codegen

check-go-toolchain:
	@./scripts/check-go-toolchain.sh

check-proto-toolchain:
	@BUF_VERSION='$(BUF_VERSION)' \
		PROTOBUF_GO_PLUGIN_REMOTE='$(PROTOBUF_GO_PLUGIN_REMOTE)' \
		PROTOBUF_GO_PLUGIN_REVISION='$(PROTOBUF_GO_PLUGIN_REVISION)' \
		GRPC_GO_PLUGIN_REMOTE='$(GRPC_GO_PLUGIN_REMOTE)' \
		GRPC_GO_PLUGIN_REVISION='$(GRPC_GO_PLUGIN_REVISION)' \
		./scripts/check-proto-toolchain.sh

test-go-toolchain-contract: check-go-toolchain
	@./scripts/tests/go-toolchain-contract-test.sh

test-go: test-go-toolchain-contract
	env -u GOFLAGS GOENV=off GOWORK=off go test -tags= ./...

test-go-postgres: check-go-toolchain
	@env -u GOFLAGS GOENV=off GOWORK=off go run ./services/external/bot-service/cmd/postgres-test-target --majors 15,16 -- go test -tags=postgres ./services/external/bot-service/internal/repository/postgres/... ./services/external/bot-service/internal/domain/service ./services/external/bot-service/internal/transport/http ./services/external/bot-service/internal/app -count=1

test-go-all:
	@$(MAKE) test-go
	@$(MAKE) test-go-postgres

test-render-evidence: check-go-toolchain
	go test ./services/external/bot-service/internal/app -run TestBotServiceRenderCountsNonEmptyObjects -count=1

tidy-go: check-go-toolchain
	go mod tidy

govulncheck: check-go-toolchain
	$(if $(filter file,$(origin GOVULNCHECK_VERSION)),,$(error GOVULNCHECK_VERSION нельзя переопределять))
	@printf 'Проверенный Go toolchain: %s\n' "$$(env -u GOFLAGS GOENV=off GOWORK=off go env GOVERSION)"
	env -u GOFLAGS GOENV=off GOWORK=off go run 'golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)' -mode=source -scan=symbol -show=traces,version ./...

gen-openapi: gen-openapi-go gen-openapi-ts

gen-openapi-go: gen-integration-gateway-openapi-go gen-interaction-gateway-openapi-go gen-control-api-gateway-openapi-go

gen-integration-gateway-openapi-go:
	oapi-codegen -config tools/codegen/openapi/integration-gateway-go.yaml contracts/openapi/integration-gateway/v1/openapi.yaml

gen-interaction-gateway-openapi-go:
	oapi-codegen -config tools/codegen/openapi/interaction-gateway-go.yaml contracts/openapi/interaction-gateway/v1/openapi.yaml

gen-control-api-gateway-openapi-go:
	oapi-codegen -config tools/codegen/openapi/control-api-gateway-go.yaml contracts/openapi/control-api-gateway/v1/openapi.yaml
	gofmt -w services/external/control-api-gateway/internal/transport/http/generated

check-control-api-gateway-asyncapi-toolchain:
	@version="$$($(ASYNCAPI) --version)"; \
	case "$$version" in \
		"$(CONTROL_API_GATEWAY_ASYNCAPI_VERSION) "*) ;; \
		*) echo "unexpected AsyncAPI CLI version: expected $(CONTROL_API_GATEWAY_ASYNCAPI_VERSION)" >&2; exit 1 ;; \
	esac

lint-control-api-gateway-asyncapi: check-control-api-gateway-asyncapi-toolchain
	$(ASYNCAPI) validate contracts/asyncapi/control-api-gateway/v1/asyncapi.yaml

gen-control-api-gateway-asyncapi: check-control-api-gateway-asyncapi-toolchain
	rm -rf services/external/control-api-gateway/internal/transport/websocket/generated
	mkdir -p services/external/control-api-gateway/internal/transport/websocket/generated
	$(ASYNCAPI) generate models golang contracts/asyncapi/control-api-gateway/v1/asyncapi.yaml \
		--packageName generated --goIncludeComments --no-interactive \
		--output services/external/control-api-gateway/internal/transport/websocket/generated
	$(MAKE) check-control-api-gateway-asyncapi-codegen

check-control-api-gateway-asyncapi-codegen:
	./tools/codegen/check-control-api-gateway-asyncapi.sh

gen-openapi-ts:
	cd services/staff/control-center && npm exec -- openapi-ts -f openapi-ts.config.mjs

lint-proto: check-proto-toolchain
	buf lint

build-proto: check-proto-toolchain
	buf build

gen-proto: check-proto-toolchain
	buf generate

check-proto-codegen: check-proto-toolchain
	@./scripts/check-proto-codegen.sh
