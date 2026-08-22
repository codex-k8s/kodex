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

.PHONY: check-go-toolchain check-sql-boundary check-proto-toolchain check-control-api-gateway-asyncapi-toolchain test-go-toolchain-contract test-web-only-release test-authority-policy-codegen test-control-plane-postgres test-go test-go-all tidy-go govulncheck gen-openapi gen-openapi-go gen-control-api-gateway-openapi-go gen-control-api-gateway-asyncapi check-control-api-gateway-asyncapi-codegen lint-control-api-gateway-asyncapi gen-openapi-ts lint-proto build-proto gen-proto check-proto-codegen

check-go-toolchain:
	@./scripts/check-go-toolchain.sh

check-sql-boundary: check-go-toolchain
	@env -u GOFLAGS GOENV=off GOWORK=off go run ./tools/check-sql-boundary

check-proto-toolchain:
	@BUF_VERSION='$(BUF_VERSION)' \
		PROTOBUF_GO_PLUGIN_REMOTE='$(PROTOBUF_GO_PLUGIN_REMOTE)' \
		PROTOBUF_GO_PLUGIN_REVISION='$(PROTOBUF_GO_PLUGIN_REVISION)' \
		GRPC_GO_PLUGIN_REMOTE='$(GRPC_GO_PLUGIN_REMOTE)' \
		GRPC_GO_PLUGIN_REVISION='$(GRPC_GO_PLUGIN_REVISION)' \
		./scripts/check-proto-toolchain.sh

test-go-toolchain-contract: check-go-toolchain
	@./scripts/tests/go-toolchain-contract-test.sh

test-web-only-release:
	@./scripts/tests/web-only-release-test.sh

test-authority-policy-codegen:
	@./scripts/tests/authority-policy-codegen-test.sh

test-control-plane-postgres:
	@./scripts/tests/control-plane-postgres-test.sh

test-go: test-go-toolchain-contract check-sql-boundary
	@./scripts/test-go-modules.sh

test-go-all:
	@$(MAKE) test-go

tidy-go: check-go-toolchain
	@for module in go.mod $$(find libs/go services -name go.mod -type f | sort); do \
		directory=$$(dirname "$$module"); \
		(cd "$$directory" && env -u GOFLAGS GOENV=off GOWORK=off go mod tidy); \
	done

govulncheck: check-go-toolchain
	$(if $(filter file,$(origin GOVULNCHECK_VERSION)),,$(error GOVULNCHECK_VERSION нельзя переопределять))
	@printf 'Проверенный Go toolchain: %s\n' "$$(env -u GOFLAGS GOENV=off GOWORK=off go env GOVERSION)"
	env -u GOFLAGS GOENV=off GOWORK=off go run 'golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)' -mode=source -scan=symbol -show=traces,version ./...

gen-openapi: gen-openapi-go gen-openapi-ts

gen-openapi-go: gen-control-api-gateway-openapi-go

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
