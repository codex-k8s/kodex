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

.PHONY: check-go-toolchain check-sql-boundary check-proto-toolchain check-openapi-toolchain check-control-api-gateway-asyncapi-toolchain test-go-toolchain-contract test-web-only-release test-service-infrastructure-bootstrap test-management-surfaces test-install-contract test-authority-policy-codegen test-control-plane-postgres test-internal-rpc-authority-postgres test-session-archive-seaweedfs-e2e test-integration-synthetic test-full-local-e2e-entrypoint test-local-e2e-oidc-group-fixture-contract test-local-backup-controller-credentials-contract test-local-go-cache-contract test-local-image-cache-import-contract test-local-kubernetes-api-egress-contract test-local-object-storage-capacity-contract test-local-provider-account-persistence-contract test-local-material-contract-revision test-integration-deployed-e2e-check test-integration-deployed-e2e test-stt-tts-service-contract test-stt-security-negative test-stt-acceptance test-go test-go-all tidy-go govulncheck gen-integration-packages check-integration-package-codegen gen-openapi gen-openapi-go gen-control-api-gateway-openapi-go gen-control-api-gateway-asyncapi check-control-api-gateway-asyncapi-codegen lint-control-api-gateway-asyncapi gen-openapi-ts lint-proto build-proto gen-proto check-proto-codegen

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

test-service-infrastructure-bootstrap:
	@./scripts/tests/service-infrastructure-bootstrap-test.sh
	@./scripts/tests/nats-operator-material-test.sh
	@./scripts/tests/local-storage-e2e-reliability-contract-test.sh

test-management-surfaces:
	@./scripts/tests/keycloak-protocol-mapper-reconcile-test.sh
	@./scripts/tests/management-surfaces-test.sh

test-install-contract:
	@./scripts/tests/install-contract-test.sh
	@./scripts/tests/ipv6-ingress-bridge-test.sh

test-authority-policy-codegen:
	@./scripts/tests/authority-policy-codegen-test.sh

test-control-plane-postgres:
	@./scripts/tests/control-plane-postgres-test.sh

test-session-archive-seaweedfs-e2e:
	@./scripts/tests/session-archive-seaweedfs-e2e-test.sh

test-internal-rpc-authority-postgres:
	@./scripts/tests/internal-rpc-authority-postgres-test.sh

test-integration-synthetic:
	@./scripts/tests/integration-synthetic-test.sh

test-full-local-e2e-entrypoint:
	@./scripts/tests/full-local-e2e-entrypoint-test.sh
	@./scripts/tests/hot-reload-verifier-contract-test.sh
	@./scripts/tests/integration-deployed-e2e-contract-test.sh
	@./scripts/tests/local-kubernetes-e2e-diagnostics-contract-test.sh
	@./scripts/tests/local-statefulset-rollout-contract-test.sh

test-local-e2e-oidc-group-fixture-contract:
	@./scripts/tests/local-e2e-oidc-group-fixture-test.sh

test-local-go-cache-contract:
	@./scripts/tests/local-go-cache-contract-test.sh

test-local-kubernetes-api-egress-contract:
	@./scripts/tests/local-kubernetes-api-egress-contract-test.sh

test-local-object-storage-capacity-contract:
	@./scripts/tests/local-object-storage-capacity-contract-test.sh

test-local-provider-account-persistence-contract:
	@./scripts/tests/local-provider-account-persistence-contract-test.sh

test-local-backup-controller-credentials-contract:
	@./scripts/tests/local-backup-controller-credentials-contract-test.sh

test-local-image-cache-import-contract:
	@./scripts/tests/local-image-cache-import-contract-test.sh

.PHONY: test-platform-worker-grant-workloads-contract
test-platform-worker-grant-workloads-contract:
	@./scripts/tests/platform-worker-grant-workloads-contract-test.sh

test-local-material-contract-revision:
	@./scripts/tests/local-material-contract-revision-test.sh

test-integration-deployed-e2e-check:
	@cd services/staff/control-center && \
		KODEX_E2E_CHECK_ONLY=1 KODEX_E2E_PROFILE=web-only KODEX_E2E_RESOURCE_PREFIX=check-only \
		./node_modules/.bin/tsc --noEmit -p tsconfig.e2e.json && \
		KODEX_E2E_CHECK_ONLY=1 KODEX_E2E_PROFILE=web-only KODEX_E2E_RESOURCE_PREFIX=check-only \
		./node_modules/.bin/playwright test --config playwright.integration.config.ts --list

test-integration-deployed-e2e:
	@./scripts/tests/integration-deployed-e2e.sh

test-stt-tts-service-contract:
	@./scripts/tests/stt-tts-service-contract-test.sh

test-stt-security-negative:
	@cd services/internal/stt-tts-service && env -u GOFLAGS GOENV=off GOWORK=off \
		go test ./internal/... -run 'TestConfigRejectsAlternateSecurityBoundary|TestPolicyProjectionRequiresExactAuthorityEcho|TestWithProjectReferenceRequiresCanonicalLocator|TestTranscribeFailsClosedBeforeProvider|TestTranscribeCapsDeadlineByAuthorityExpiry|TestValidateAudioRejectsTrailingWAVDataAndHeaderOnlyFLAC|TestTranscribeRejectsMissingVerifiedContext|TestServerBoundsConcurrentTranscriptions|TestTranscribeRejectsProviderDiagnostics|TestTransportErrorDoesNotExposeProviderDiagnostics|TestTransportErrorPreservesRequestCancellation'

test-stt-acceptance:
	@cd services/internal/stt-tts-service && env -u GOFLAGS GOENV=off GOWORK=off \
		go test ./internal/clients/openai -run '^TestLiveRussianNumberFixture$$' -v

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

gen-integration-packages: check-go-toolchain
	@cd libs/go/integrationpackage && env -u GOFLAGS GOENV=off GOWORK=off go run ./cmd/packagegen \
		-contracts ../../../contracts/integrations/v1/definitions -output shipped_gen.go
	@gofmt -w libs/go/integrationpackage/shipped_gen.go

check-integration-package-codegen: check-go-toolchain
	@tmp=$$(mktemp); trap 'rm -f "$$tmp"' EXIT; \
		cd libs/go/integrationpackage && env -u GOFLAGS GOENV=off GOWORK=off go run ./cmd/packagegen \
		-contracts ../../../contracts/integrations/v1/definitions -output "$$tmp"; \
		gofmt -w "$$tmp"; cmp -s shipped_gen.go "$$tmp" || { echo 'integration package generated code is stale' >&2; exit 1; }

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
