GOVULNCHECK_VERSION := v1.6.0
GO_MIN_VERSION := 1.26.6
GO_TOOLCHAIN := go1.26.6
BUF_VERSION := 1.71.0
PROTOBUF_GO_PLUGIN_REMOTE := buf.build/protocolbuffers/go:v1.36.11
PROTOBUF_GO_PLUGIN_REVISION := 1
GRPC_GO_PLUGIN_REMOTE := buf.build/grpc/go:v1.6.2
GRPC_GO_PLUGIN_REVISION := 1
PROTOBUF_GO_PLUGIN_LOCAL_VERSION := 1.36.11
GRPC_GO_PLUGIN_LOCAL_VERSION := 1.6.2
CONTROL_API_GATEWAY_ASYNCAPI_PARSER_VERSION := 3.6.3
OAPI_CODEGEN_VERSION := v2.7.1

.PHONY: check-go-toolchain check-sql-boundary check-proto-toolchain check-openapi-toolchain check-control-api-gateway-asyncapi-toolchain test-go-toolchain-contract test-web-only-release test-authority-policy-codegen test-control-plane-postgres test-go test-go-all tidy-go govulncheck gen-openapi gen-openapi-go gen-control-api-gateway-openapi-go gen-control-api-gateway-asyncapi check-control-api-gateway-asyncapi-codegen lint-control-api-gateway-asyncapi gen-openapi-ts lint-proto build-proto gen-proto check-proto-codegen

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

check-openapi-toolchain:
	@OAPI_CODEGEN_VERSION='$(OAPI_CODEGEN_VERSION)' ./scripts/check-openapi-toolchain.sh

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
	@GOVULNCHECK_VERSION='$(GOVULNCHECK_VERSION)' ./scripts/govulncheck-go-modules.sh

gen-openapi: gen-openapi-go gen-openapi-ts

gen-openapi-go: gen-control-api-gateway-openapi-go

gen-control-api-gateway-openapi-go: check-openapi-toolchain
	oapi-codegen -config tools/codegen/openapi/control-api-gateway-go.yaml contracts/openapi/control-api-gateway/v1/openapi.yaml
	gofmt -w services/external/control-api-gateway/internal/transport/http/generated

check-control-api-gateway-asyncapi-toolchain:
	@cd services/staff/control-center && node tools/generate-asyncapi.mjs --check-toolchain
	@cd services/staff/control-center && \
		test "$$(node -p 'require("./node_modules/@asyncapi/parser/package.json").version')" = "$(CONTROL_API_GATEWAY_ASYNCAPI_PARSER_VERSION)"

lint-control-api-gateway-asyncapi: check-control-api-gateway-asyncapi-toolchain
	cd services/staff/control-center && npm run validate:asyncapi

gen-control-api-gateway-asyncapi: check-control-api-gateway-asyncapi-toolchain
	cd services/staff/control-center && npm run generate:asyncapi
	gofmt -w services/external/control-api-gateway/internal/transport/websocket/generated
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
	@PROTOBUF_GO_PLUGIN_LOCAL_VERSION='$(PROTOBUF_GO_PLUGIN_LOCAL_VERSION)' \
		GRPC_GO_PLUGIN_LOCAL_VERSION='$(GRPC_GO_PLUGIN_LOCAL_VERSION)' \
		./scripts/generate-proto.sh

check-proto-codegen: check-proto-toolchain
	@PROTOBUF_GO_PLUGIN_LOCAL_VERSION='$(PROTOBUF_GO_PLUGIN_LOCAL_VERSION)' \
		GRPC_GO_PLUGIN_LOCAL_VERSION='$(GRPC_GO_PLUGIN_LOCAL_VERSION)' \
		./scripts/check-proto-codegen.sh
