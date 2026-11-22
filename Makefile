GOVULNCHECK_VERSION := v1.6.0

.PHONY: test-node-runner-cache
test-node-runner-cache:
	@timeout 30s python3 -m unittest discover -s tools/dev -p 'test_node_runner*.py'
GO_MIN_VERSION := 1.26.10
GO_TOOLCHAIN := go1.26.10

BUF_VERSION := 1.71.0
PROTOBUF_GO_PLUGIN_REMOTE := buf.build/protocolbuffers/go:v1.36.11
PROTOBUF_GO_PLUGIN_REVISION := 1
GRPC_GO_PLUGIN_REMOTE := buf.build/grpc/go:v1.6.2
GRPC_GO_PLUGIN_REVISION := 1
PROTOBUF_GO_PLUGIN_LOCAL_VERSION := 1.36.11
GRPC_GO_PLUGIN_LOCAL_VERSION := 1.6.2
CONTROL_API_GATEWAY_ASYNCAPI_PARSER_VERSION := 3.6.3
OAPI_CODEGEN_VERSION := v2.7.1

.PHONY: check-go-toolchain check-sql-boundary check-proto-toolchain check-openapi-toolchain check-control-api-gateway-asyncapi-toolchain test-go-toolchain-contract test-web-only-release test-service-infrastructure-bootstrap test-management-surfaces test-install-contract test-authority-policy-codegen test-internal-rpc-authority-abi-render test-control-plane-postgres test-internal-rpc-authority-postgres test-session-archive-seaweedfs-e2e test-integration-synthetic test-full-local-e2e-entrypoint test-local-e2e-oidc-group-fixture-contract test-local-backup-controller-credentials-contract test-local-go-cache-contract test-local-image-cache-import-contract test-local-kubernetes-api-egress-contract test-local-object-storage-capacity-contract test-local-provider-account-persistence-contract test-local-material-contract-revision test-integration-deployed-e2e-check test-integration-deployed-e2e test-agent-runner test-stt-tts-service-contract test-stt-security-negative test-stt-provider-smoke test-go test-go-all tidy-go govulncheck gen-integration-packages check-integration-package-codegen gen-openapi gen-openapi-go gen-control-api-gateway-openapi-go gen-control-api-gateway-asyncapi check-control-api-gateway-asyncapi-codegen lint-control-api-gateway-asyncapi gen-openapi-ts lint-proto build-proto gen-proto check-proto-codegen

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

.PHONY: test-oci-publisher
test-oci-publisher:
	@cd tools/release/oci-publisher && env -u GOFLAGS GOENV=off GOWORK=off timeout 120s go test -count=1 -timeout=90s ./...
	@timeout 60s python3 -B tools/release/buildctl-chunked-test.py

test-portable-services:
	@timeout 90s python3 -B scripts/tests/portable-services-test.py

.PHONY: test-portable-services

.PHONY: test-portable-services-native
test-portable-services-native:
	@timeout 300s python3 -B scripts/tests/portable-services-native-test.py

.PHONY: test-provider-sandbox-native
test-provider-sandbox-native:
	@timeout 90s python3 -B tools/install/test_provider_sandbox_fence_native.py

.PHONY: test-native-integration-egress
test-native-integration-egress: check-go-toolchain
	@timeout 30s python3 -B tools/install/test_native_integration_egress.py
	@timeout 30s python3 -B tools/install/test_observability_qualification.py
	@bash scripts/tests/native-integration-egress-test.sh

test-web-only-release:
	@timeout 30s python3 -B tools/install/test_image_policy_inputs.py
	@timeout 30s python3 -B tools/install/test_native_integration_egress.py
	@timeout 30s python3 -B tools/install/test_observability_qualification.py
	@timeout 30s python3 -B tools/install/test_provider_sandbox_profile.py
	@timeout 30s python3 -B tools/install/test_provider_sandbox_transition.py
	@timeout 30s python3 -B tools/install/test_bootstrap_completion.py
	@timeout 30s python3 -B tools/install/test_bootstrap_migration_custody.py
	@timeout 30s python3 -B tools/install/test_bootstrap_migration_rollout.py
	@timeout 30s python3 -B tools/install/test_bootstrap_upgrade.py
	@timeout 60s python3 -B tools/install/test_bootstrap_image_upgrade.py
	@timeout 30s python3 -B tools/install/test_upgrade_delivery.py
	@timeout 30s python3 -B tools/install/test_assistant_readback.py
	@timeout 30s python3 -B tools/install/test_oidc_authority_profile.py
	@timeout 30s python3 -B tools/install/test_component_image_profile.py
	@timeout 30s python3 -B tools/install/test_egress_secret_access.py
	@timeout 30s python3 -B tools/install/test_clamav_profile.py
	@timeout 30s python3 -B tools/install/test_kubernetes_api_profile.py
	@timeout 30s python3 -B tools/install/test_buildkit_runtime_profile.py
	@timeout 30s python3 -B tools/install/test_email_projection.py
	@timeout 30s python3 -B tools/install/test_authority_registry.py
	@timeout 30s python3 -B tools/install/test_observability_profile.py
	timeout 60 python3 -B tools/release/build-recipe-test.py
	# Контур выполняет два рендера с отдельным лимитом 150 секунд на каждый.
	timeout 330 python3 -B tools/install/test_registry_source.py
	@timeout 600s python3 -B tools/install/test_model_provider.py
	@timeout 30s node --test tools/release/build-release-inventory.test.mjs tools/release/build-release-pool.test.mjs
	@timeout 120s python3 -B tools/install/test_deployment_profile.py
	@timeout 60s python3 -B tools/install/test_portable_state.py
	@timeout 60s node --test tools/release/oidc-endpoint.test.mjs tools/release/authority-policy-environment.test.mjs
	@python3 scripts/tests/pwa-public-assets-test.py
	@./scripts/tests/web-only-release-test.sh

.PHONY: test-model-catalog-authority-delivery
test-model-catalog-authority-delivery: check-go-toolchain
	@timeout 180s node --test tools/release/migration-owner-database.test.mjs tools/release/authority-catalog-transition.test.mjs tools/release/authority-catalog-transition.cli.test.mjs tools/release/authority-publisher-image.test.mjs tools/release/authority-rotation-transition.test.mjs tools/release/authority-rotation-source-delivery-model.test.mjs tools/release/authority-rotation-source-delivery.cli.test.mjs tools/release/authority-policy-environment.test.mjs tools/release/control-plane-policy-projection.test.mjs tools/release/portable-runner-target.test.mjs tools/release/component-api-maintenance.test.mjs tools/release/component-api-traffic.test.mjs tools/release/component-api-maintenance-cli.test.mjs tools/deploy/control-plane-pre-migration-backup.test.mjs
	@cd services/internal/control-plane && timeout 120s go test -count=1 -p 1 ./internal/domain/service/authorityproof -run '^TestCatalogBinaryStartsAndIssuesLegacyProofWithPolicy105$$'
	@cd services/internal/internal-rpc-authority && timeout 120s go test -count=1 -p 1 ./cmd/cli -run '^TestRotationStatusKeepsRegistryAndSnapshotSequencesIndependent$$'

.PHONY: test-service-generation-postgres
test-service-generation-postgres: check-go-toolchain
	@./scripts/tests/control-plane-postgres-test.sh '^(TestServiceCredentialColdStartComponent|TestWorkerGrantInstancesComponent|TestTrustedWorkloadGenerationComponent)$$'

.PHONY: test-workspace-policy-contract
test-workspace-policy-contract: check-go-toolchain
	@cd tools/dev/workspace-policy-contract && env -u GOFLAGS GOENV=off GOWORK=off timeout 120s go test -count=1 -timeout=60s ./...

.PHONY: test-runtime-admission-gate
test-runtime-admission-gate:
	@timeout 60s sh -c 'python3 -B tools/dev/test_runtime_admission_gate.py && python3 -B tools/dev/test_transition_runtime_initializer_mounts.py'

.PHONY: test-pwa-public-assets-http
test-pwa-public-assets-http:
	@timeout 180s python3 scripts/tests/pwa-public-assets-test.py --http

.PHONY: test-frontend-offline-bootstrap
test-frontend-offline-bootstrap:
	@timeout 900s python3 scripts/tests/frontend-offline-bootstrap-test.py

test-service-infrastructure-bootstrap:
	@./scripts/tests/service-infrastructure-bootstrap-test.sh
	@./scripts/tests/nats-operator-material-test.sh
	@./scripts/tests/local-storage-e2e-reliability-contract-test.sh

test-management-surfaces:
	@timeout 30s python3 -m unittest discover -s tools/deploy -p 'test_keycloak*.py'
	@node --test tools/release/proxy-session-cookies.test.mjs
	@./scripts/tests/keycloak-protocol-mapper-reconcile-test.sh
	@./scripts/tests/keycloak-group-reconcile-test.sh
	@./scripts/tests/management-surfaces-test.sh

.PHONY: test-install-workload-startup
test-install-workload-startup:
	@python3 scripts/tests/install-workload-startup-test.py

test-install-contract:
	@timeout 30s python3 -m unittest discover -s tools/install -p 'test_dns_address_normalization.py'
	@timeout 30s python3 -m unittest discover -s tools/install -p 'test_ensure_namespace.py'
	@timeout 30s python3 -m unittest discover -s tools/install -p 'test_materialize_secrets_context.py'
	@timeout 30s python3 -m unittest discover -s tools/install -p 'test_local_object_storage_tls.py'
	@./scripts/tests/install-contract-test.sh
	@./scripts/tests/ipv6-ingress-bridge-test.sh
	@$(MAKE) --no-print-directory test-scan-memory-budget
	@$(MAKE) --no-print-directory test-installation-cleanup

.PHONY: test-installation-cleanup
test-installation-cleanup:
	@timeout 60s python3 -m unittest discover -s tools/dev -p 'test_installation_*.py'
	@cd tools/dev/installation-runtime && GOTOOLCHAIN=go1.26.10 go test -race -timeout=60s ./...

.PHONY: test-scan-memory-budget
test-scan-memory-budget:
	@python3 scripts/tests/scan-memory-budget-test.py

.PHONY: test-worker-strategy-upgrade
test-worker-strategy-upgrade:
	@timeout 180s python3 scripts/tests/worker-strategy-upgrade-test.py

.PHONY: test-runtime-orphan-api
test-runtime-orphan-api:
	@CGO_ENABLED=0 timeout 240s python3 scripts/tests/runtime-orphan-api-test.py

.PHONY: test-runtime-orphan-postgres
test-runtime-orphan-postgres:
	@CGO_ENABLED=0 timeout 240s python3 scripts/tests/runtime-orphan-postgres-test.py

test-authority-policy-codegen:
	@./scripts/tests/authority-policy-codegen-test.sh

test-internal-rpc-authority-abi-render:
	@./scripts/tests/internal-rpc-authority-abi-render-test.sh

.PHONY: test-worker-authority-projections
test-worker-authority-projections:
	@./scripts/tests/worker-authority-projections-test.sh

.PHONY: test-interaction-gateway-render test-interaction-gateway-postgres
test-interaction-gateway-render:
	@./scripts/tests/interaction-gateway-render-test.sh

test-interaction-gateway-postgres:
	@./scripts/tests/control-plane-postgres-test.sh '^TestBootstrapComponent$$/(interaction|integration_connection_tests)'

.PHONY: test-workflow-server-postgres
test-workflow-server-postgres: check-go-toolchain
	@timeout --kill-after=10s 240s bash scripts/tests/workflow-server-postgres-test.sh

.PHONY: test-workflow-exception-context-postgres
test-workflow-exception-context-postgres: check-go-toolchain
	@timeout --kill-after=10s 300s bash scripts/tests/control-plane-postgres-test.sh '^(TestWorkflowExceptionContextComponent|TestWorkflowExceptionContextMigrationProofComponent|TestServerWorkflowMigrationProofComponent|TestLegacyWorkflowReworkMigrationProofComponent|TestWorkflowExceptionRecoveryComponent|TestServerWorkflowRetrySnapshotComponent)$$'

.PHONY: test-workflow-upstream-rework-postgres
test-workflow-upstream-rework-postgres: check-go-toolchain
	@timeout --kill-after=10s 300s bash scripts/tests/control-plane-postgres-test.sh '^(TestWorkflowUpstreamReworkComponent|TestWorkflowUpstreamReworkMigrationProofComponent|TestWorkflowExceptionContextComponent|TestWorkflowExceptionContextMigrationProofComponent|TestServerWorkflowMigrationProofComponent|TestLegacyWorkflowReworkMigrationProofComponent)$$'

.PHONY: test-workflow-artifact-query-postgres
test-workflow-artifact-query-postgres: check-go-toolchain
	@timeout --kill-after=10s 240s bash scripts/tests/control-plane-postgres-test.sh '^TestWorkflowArtifactQueryBoundaryComponent$$'

test-control-plane-postgres:
	@bash scripts/tests/control-plane-postgres-test.sh --artifact-optional-antivirus-upgrade
	@bash scripts/tests/control-plane-postgres-test.sh --integration-resource-policies-upgrade
	@bash scripts/tests/control-plane-postgres-test.sh --integration-resource-policies-fresh-install
	@python3 -B tools/release/generate-authority-proof-counter-proof.py --check
	@bash scripts/tests/control-plane-postgres-test.sh --authority-proof-counter-upgrade
	@bash scripts/tests/control-plane-postgres-test.sh --authority-proof-counter-fresh-install
	@./scripts/tests/control-plane-postgres-test.sh

.PHONY: test-personal-integration-token-allowance-postgres
.PHONY: test-personal-integration-probe-allowance-postgres
test-personal-integration-probe-allowance-postgres: check-go-toolchain
	@KODEX_TEST_POSTGRES_STORAGE=ram ./scripts/tests/control-plane-postgres-test.sh '^TestPersonalIntegrationCredentialProbeDeadlineComponent$$'

test-personal-integration-token-allowance-postgres: check-go-toolchain
	@timeout --kill-after=10s 300s env KODEX_TEST_POSTGRES_STORAGE=ram ./scripts/tests/control-plane-postgres-test.sh '^TestPersonalIntegrationInvocationDeadlineComponent$$'

.PHONY: test-personal-integration-credentials-postgres
test-personal-integration-credentials-postgres: check-go-toolchain
	@timeout --kill-after=10s 240s bash scripts/tests/control-plane-postgres-test.sh --personal-credentials-upgrade

.PHONY: test-impact-owner-snapshot-upgrade
test-impact-owner-snapshot-upgrade:
	@./scripts/tests/impact-owner-snapshot-upgrade-test.sh

test-session-archive-seaweedfs-e2e:
	@./scripts/tests/session-archive-seaweedfs-e2e-test.sh

test-internal-rpc-authority-postgres:
	@./scripts/tests/internal-rpc-authority-postgres-test.sh

.PHONY: test-authority-executable-distroless
test-authority-executable-distroless: check-go-toolchain
	@./scripts/tests/authority-executable-distroless-test.sh

test-integration-synthetic:
	@./scripts/tests/integration-synthetic-test.sh

.PHONY: test-integration-hot-reload-container
test-integration-hot-reload-container:
	@./scripts/tests/integration-hot-reload-container-test.sh

test-full-local-e2e-entrypoint:
	bash scripts/tests/smoke-private-output-contract-test.sh
	@node --test tools/dev/component-manifest.test.mjs tools/dev/source-git-trust.test.mjs tools/dev/prepare-operator-manifest.test.mjs tools/dev/runner-seed-cli.test.mjs
	@./scripts/tests/full-local-e2e-entrypoint-test.sh
	@./scripts/tests/local-profile-selection-test.sh
	@./scripts/tests/hot-reload-verifier-contract-test.sh
	@./scripts/tests/integration-deployed-e2e-contract-test.sh
	@./scripts/tests/local-kubernetes-e2e-diagnostics-contract-test.sh
	@./scripts/tests/local-statefulset-rollout-contract-test.sh
	@bash scripts/tests/local-mail-projection-contract-test.sh

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

.PHONY: test-provider-bootstrap-postgres
test-provider-bootstrap-postgres:
	@timeout 300s python3 -B scripts/tests/provider-bootstrap-postgres-test.py

test-local-backup-controller-credentials-contract:
	@./scripts/tests/local-backup-controller-credentials-contract-test.sh

.PHONY: test-local-backup-controller-build-contract
test-local-backup-controller-build-contract:
	@timeout 60s python3 -B tools/dev/test_build_local_backup_controller.py

test-local-image-cache-import-contract:
	@./scripts/tests/local-image-cache-import-contract-test.sh

.PHONY: test-platform-worker-grant-workloads-contract
test-platform-worker-grant-workloads-contract:
	@./scripts/tests/platform-worker-grant-workloads-contract-test.sh

test-local-material-contract-revision:
	@./scripts/tests/local-material-contract-revision-test.sh

.PHONY: test-secret-draft-key-recovery-api
test-secret-draft-key-recovery-api:
	@timeout 360s python3 -B scripts/tests/secret-draft-key-recovery-api-test.py

test-integration-deployed-e2e-check:
	@cd services/staff/control-center && \
		KODEX_E2E_CHECK_ONLY=1 KODEX_E2E_PROFILE=web-only KODEX_E2E_RESOURCE_PREFIX=check-only \
		./node_modules/.bin/tsc --noEmit -p tsconfig.e2e.json && \
		KODEX_E2E_CHECK_ONLY=1 KODEX_E2E_PROFILE=web-only KODEX_E2E_RESOURCE_PREFIX=check-only \
		./node_modules/.bin/playwright test --config playwright.integration.config.ts --list

test-integration-deployed-e2e:
	@./scripts/tests/integration-deployed-e2e.sh

test-runtime-mcp-catalog:
	@./scripts/tests/runtime-mcp-catalog-test.sh

.PHONY: test-runtime-mcp-catalog

test-agent-runner:
	@./scripts/tests/agent-runner-test.sh

.PHONY: test-opencode-native
test-opencode-native: check-go-toolchain
	@timeout 30s node --test services/jobs/agent-runner/internal/opencode/model_measurement.test.mjs
	@test -n "$${KODEX_TEST_OPENCODE_BINARY:-}" || { echo 'KODEX_TEST_OPENCODE_BINARY must select the pinned qualification executable' >&2; exit 1; }
	@./scripts/tests/opencode-template-native-test.sh

.PHONY: test-opencode-native-postgres
test-opencode-native-postgres: check-go-toolchain
	@./scripts/tests/control-plane-postgres-test.sh --opencode-native-upgrade

.PHONY: test-opencode-cluster-qualification
test-opencode-cluster-qualification:
	@timeout 30s python3 tools/dev/test_opencode_cluster_qualification.py

.PHONY: test-runtime-controller-artifact-transfer
test-runtime-controller-artifact-transfer:
	@bash ./scripts/tests/runtime-controller-artifact-transfer-test.sh

.PHONY: test-runtime-workspace-acceptance
test-runtime-workspace-acceptance:
	@timeout 30s node --test tools/dev/runtime-workspace-acceptance.test.mjs tools/dev/runtime-provider-catalog.test.mjs

.PHONY: test-stt-http-acceptance
test-stt-http-acceptance:
	@timeout 30s node --test tools/dev/stt-http-acceptance.test.mjs tools/dev/owner-session-client.test.mjs

.PHONY: test-owner-session-acceptance
test-owner-session-acceptance:
	@timeout 30s node --test tools/dev/owner-session-client.test.mjs

.PHONY: test-provider-api-key-acceptance
test-provider-api-key-acceptance:
	@timeout 30s node --test tools/dev/provider-api-key-acceptance.test.mjs tools/dev/owner-session-client.test.mjs

.PHONY: test-stt-fixture-setup
test-stt-fixture-setup:
	@timeout 30s node --test tools/dev/stt-fixture-setup.test.mjs tools/dev/provider-api-key-acceptance.test.mjs tools/dev/owner-session-client.test.mjs

test-stt-tts-service-contract:
	@./scripts/tests/stt-tts-service-contract-test.sh

test-stt-security-negative:
	@cd services/internal/stt-tts-service && env -u GOFLAGS GOENV=off GOWORK=off \
		go test ./internal/... -run 'TestConfigRejectsAlternateSecurityBoundary|TestGRPCReadinessUsesSameSnapshotForVerifierAndSpoolDegradation|TestPrincipalUsesAuthoritySourceRevision|TestPrincipalRejectsInvalidSourceRevision|TestPrincipalRejectsMissingIdentityProvenance|TestPolicyProjectionRequiresExactAuthorityEcho|TestPolicyProjectionRejectsUnsignedLimitOverflow|TestProjectionRequiresDelegatedProofBeforeRPC|TestDelegatedProofFailsClosedBeforeRPC|TestTranscribeFailsClosedBeforeProvider|TestTranscribeCapsDeadlineByAuthorityExpiry|TestTranscribeDoesNotWidenTransportDeadline|TestReadinessAndDiagnosticHaveNoRemoteEffect|TestValidateAudioRejectsTrailingWAVDataAndHeaderOnlyFLAC|TestTranscribeRejectsMissingVerifiedContext|TestServerBoundsConcurrentTranscriptions|TestStreamAdmissionReservesBeforeFirstMessageAndReleasesOnTimeout|TestStreamAdmissionCapsStalledStreamsBeforeHandler|TestStreamAdmissionReleasesBytesWhenStalledAfterMetadata|TestStreamAdmissionDeadlineIsCappedByAuthorityExpiry|TestReceiveAudioRequiresExactCommitAndNoTrailingMessage|TestTransportErrorDoesNotExposeProviderDiagnostics|TestTransportErrorPreservesRequestCancellation|TestLabelsUseClosedSets'

test-stt-provider-smoke:
	@cd services/internal/stt-tts-service && env -u GOFLAGS GOENV=off GOWORK=off \
		go test ./internal/providersmoke -run '^TestLiveProviderRussianNumberFixture$$' -v

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

.PHONY: gen-email-bridge check-email-bridge-codegen test-email-bridge test-email-bridge-unit test-email-bridge-render test-email-bridge-install
test-email-bridge-unit: check-go-toolchain
	@cd libs/go/emailbridgeapi && go test -race -timeout 60s ./...
	@cd services/internal/email-bridge && go test -race -timeout 90s ./...
	@cd services/external/integration-gateway && go test -race -timeout 60s ./internal/integration -run 'TestEmail|Test(EveryAdvertisedOperation|EveryMutationPreservesUnknownOutcome|ScopeDeniedBeforeCredentialRead|ReadOperationsHandleRateLimits)/email'

test-email-bridge:
	@bash scripts/tests/email-bridge-test.sh

.PHONY: test-email-projection-bootstrap
test-email-projection-bootstrap:
	@timeout 30s python3 -B tools/install/test_email_projection.py
	@timeout 60s bash scripts/tests/local-email-entrypoint-contract-test.sh

.PHONY: test-artifact-retention-grant-postgres
test-artifact-retention-grant-postgres: check-go-toolchain
	@timeout 480s bash scripts/tests/control-plane-postgres-test.sh --artifact-retention-grant-upgrade

.PHONY: test-artifact-retention-claim-postgres
test-artifact-retention-claim-postgres: check-go-toolchain
	@timeout 480s bash scripts/tests/control-plane-postgres-test.sh --artifact-retention-claim-types

.PHONY: test-personal-email-owner-development
test-personal-email-owner-development: check-go-toolchain
	@timeout 900s bash scripts/tests/personal-email-owner-test.sh

.PHONY: test-personal-integrations-admission-local
test-personal-integrations-admission-local:
	@timeout --kill-after=10s 240s node --test --test-concurrency=1 tools/release/personal-integrations-authority-owner.test.mjs tools/release/personal-integrations-authority-predecessor.test.mjs tools/release/personal-integrations-material-source.test.mjs tools/release/personal-integrations-authority-image.test.mjs tools/release/personal-integrations-stopped-admission.test.mjs tools/release/personal-integrations-staged-policy.test.mjs tools/release/personal-integrations-staged-inputs.test.mjs tools/release/personal-integrations-staged-admission.test.mjs tools/release/personal-integrations-deployment-inputs.test.mjs tools/release/personal-integrations-cp-policy.test.mjs tools/release/artifact-retention-issuer-inputs.test.mjs tools/release/artifact-retention-issuer-migration.test.mjs tools/release/artifact-retention-issuer-assets.test.mjs tools/release/component-publication.test.mjs

.PHONY: test-personal-integrations-authority-local
test-personal-integrations-authority-local:
	@timeout --kill-after=10s 240s node --test --test-concurrency=1 --test-timeout=180000 tools/release/personal-integrations-authority-api.test.mjs tools/release/personal-integrations-authority-binding.test.mjs tools/release/personal-integrations-owner-state.test.mjs tools/release/personal-integrations-installed-custody.test.mjs tools/release/personal-integrations-migration-witness.test.mjs tools/release/personal-integrations-rotation-admission.test.mjs tools/release/personal-integrations-authority-late.test.mjs tools/release/personal-integrations-rotation-cli.test.mjs tools/release/authority-rotation-transition.test.mjs tools/release/authority-api-scope.test.mjs tools/release/authority-telegram-transition.test.mjs

.PHONY: test-personal-integrations-migration-recovery
test-personal-integrations-migration-recovery:
	@timeout --kill-after=5s 60s node --test --test-timeout=30000 --test-concurrency=1 tools/release/personal-integrations-migration-recovery.test.mjs tools/release/personal-integrations-migration.test.mjs tools/release/personal-integrations-staged-policy.test.mjs tools/release/artifact-retention-grant-catalog.test.mjs tools/release/personal-integrations-artifact-spec.test.mjs

.PHONY: test-artifact-retention-grant-catalog-cli-local
test-artifact-retention-grant-catalog-cli-local:
	@timeout --kill-after=10s 360s node --test --test-concurrency=1 tools/release/artifact-retention-grant-catalog-cli.test.mjs

.PHONY: test-personal-integrations-migration-release
test-personal-integrations-migration-release:
	@timeout --kill-after=15s 1500s node --test --test-concurrency=2 tools/release/personal-integrations-migration.test.mjs tools/release/personal-integrations-migration-recovery.test.mjs tools/release/control-plane-immutable-migration.test.mjs tools/release/portable-component-cohort.test.mjs tools/release/backup-controller-maintenance-cli.test.mjs tools/release/component-api-traffic.test.mjs tools/release/component-publication.test.mjs tools/release/workflow-file-provenance-isolation-witness.test.mjs

.PHONY: capture-integration-cohort-schema test-integration-cohort-capture-contract
capture-integration-cohort-schema: check-go-toolchain
	@timeout --kill-after=15s 3000s bash scripts/tests/integration-cohort-schema-capture.sh

test-integration-cohort-capture-contract: check-go-toolchain
	@cd services/internal/control-plane && timeout 240s go test -p 1 -count=1 -timeout=60s ./cmd/cli -run '^TestIntegrationCohortCapture(OutputCustody|HistorySeal)$$'

.PHONY: test-personal-email-protocol-native
test-personal-email-protocol-native: check-go-toolchain
	@cd services/internal/email-bridge && timeout 600s go test -race -p 1 -count=1 -timeout=120s ./internal/component -run '^(TestPersonalPasswordProfilesNativeTLS|TestPersonalMailboxReadHandoffDeadline)$$'

.PHONY: test-personal-email-operations
test-personal-email-operations: check-go-toolchain
	@cd libs/go/controlplaneclient && timeout 180s go test -race -p 1 -count=1 -timeout=120s ./cmd/policygen -run '^TestPersonalEmailProfilesPinOnlyOwnerSetup$$'

test-email-bridge-render:
	@bash scripts/tests/email-bridge-render-test.sh

.PHONY: test-email-projection-render
test-email-projection-render:
	@bash scripts/tests/email-projection-render-test.sh

test-email-bridge-install:
	@timeout 420 bash scripts/tests/email-bridge-install-test.sh
.PHONY: test-integration-gateway-render
.PHONY: test-integration-gateway-postgres
test-integration-gateway-postgres:
	@bash scripts/tests/control-plane-postgres-test.sh '^TestBootstrapComponent$$/(catalog_owner_probe|model_catalog_is_version_bound|provider_credential_refresh|integration|role_image|runtime_environment_lifecycle|managed_configuration)'

test-integration-gateway-render:
	@bash scripts/tests/integration-gateway-render-test.sh

gen-email-bridge: check-openapi-toolchain
	oapi-codegen -config tools/codegen/openapi/email-bridge-go.yaml -o libs/go/emailbridgeapi/api.gen.go contracts/openapi/email-bridge/v1/openapi.yaml
	node tools/codegen/email-bridge-schema.mjs

check-email-bridge-codegen: check-openapi-toolchain
	@bash scripts/tests/email-bridge-codegen-test.sh

check-integration-package-codegen: check-go-toolchain
	@tmp=$$(mktemp); trap 'rm -f "$$tmp"' EXIT; \
		cd libs/go/integrationpackage && env -u GOFLAGS GOENV=off GOWORK=off go run ./cmd/packagegen \
		-contracts ../../../contracts/integrations/v1/definitions -output "$$tmp"; \
		gofmt -w "$$tmp"; cmp -s shipped_gen.go "$$tmp" || { echo 'integration package generated code is stale' >&2; exit 1; }
	@python3 -B tools/release/generate-jira-create-fields-proof.py --check

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
	cd services/staff/control-center && npm run generate:openapi

.PHONY: test-contract-registry
.PHONY: test-broker-identity-gateway
test-broker-identity-gateway: check-go-toolchain
	@cd services/external/integration-gateway && timeout 300s go test -race -count=1 -p=1 -timeout=120s -run '^Test(BrokerIdentity|PersonalCredentialWorker|PersonalProbe|KubernetesDelivery|KubernetesHealthOnly)' ./internal/app
	@cd services/external/integration-gateway && timeout 120s go test -race -count=1 -p=1 -timeout=60s -run '^TestMainFailsOnCancelledLifecycleFailure$$' ./cmd/integration-gateway
	@timeout 120s python3 -B scripts/tests/integration-gateway-lifecycle-render-test.py

.PHONY: test-managed-kubernetes-input
test-managed-kubernetes-input: check-go-toolchain
	cd libs/go/managedkubernetes && timeout 180s go test -race -p 1 -timeout 120s ./...

.PHONY: test-managed-kubernetes-capture
test-managed-kubernetes-capture: check-go-toolchain
	cd libs/go/serviceruntime && timeout 240s go test -race -p 1 -timeout 90s ./...
	cd libs/go/managedkubernetes && timeout 240s go test -race -p 1 -timeout 90s -run 'Test(Capture|NormalizeCopy)' ./...
	cd libs/go/objectstorage && timeout 240s go test -race -p 1 -timeout 90s ./...
	cd services/jobs/artifact-retention && timeout 240s go test -race -p 1 -timeout 90s ./...
	cd services/internal/control-plane && timeout 240s go test -race -p 1 -timeout 90s -run 'Test(CapturePreparedBody|PreparedContentSignedS3Reader|KubernetesCapture|KubernetesStagingCleanup|KubernetesInvocationReceipt|KubernetesStreamInputPublic)' ./internal/repository/postgres/platform ./internal/transport/grpc ./internal/domain/service/artifactpolicy
	cd services/internal/control-plane && timeout 240s go test -race -p 1 -timeout 90s ./internal/app
	cd services/external/control-api-gateway && timeout 240s go test -race -p 1 -timeout 90s -run 'TestKubernetes(CapturePublicProjection|StagingCleanup|StagingQuotaProblem|StreamInput)' ./internal/transport/http
	cd services/external/integration-gateway && timeout 240s go test -race -p 1 -timeout 90s -run 'TestKubernetes(OwnerCapturePending|ExecutionWaitsOnlyForTypedCapture|Completion)' ./internal/app ./internal/integration

.PHONY: test-managed-kubernetes-resource
test-managed-kubernetes-resource: check-go-toolchain
	cd services/internal/secret-broker && timeout 180s go test -race -p 1 -timeout 120s ./internal/integration/kubernetestarget ./internal/kubernetes

.PHONY: test-managed-kubernetes-consumer
test-managed-kubernetes-consumer: test-managed-kubernetes-input
	cd libs/go/integrationpackage && timeout 1800s go test -race -p 1 -timeout 1500s ./...
	cd services/external/integration-gateway && timeout 1800s go test -race -p 1 -timeout 120s ./internal/integration -run '^TestKubernetes|^TestNewUsesOnlyExactProviderEndpoints|^TestScopeDeniedBeforeCredentialRead'

.PHONY: test-kubernetes-grant-contract
test-kubernetes-grant-contract: check-go-toolchain
	cd libs/go/internalrpcauth && timeout 180s go test -race -p 1 -timeout 120s ./... -run '^TestDecodeExactJSON'
	cd services/external/control-api-gateway && timeout 600s go test -race -p 1 -timeout 120s ./internal/transport/http -run 'TestIntegrationGrantJSON|TestKubernetesGrantCandidateProjection|TestSystemAssistantGrantCandidateProjection|TestProjectAssistant|TestKubernetesConfigurationPlan'
	cd services/internal/control-plane && timeout 600s go test -race -p 1 -timeout 120s ./internal/transport/grpc -run 'TestProjectAssistant.*Scope|TestProjectAssistant.*GitHub|TestProjectAssistantKubernetesCandidate|TestPersonal.*Permission|TestKubernetesConfigurationPlan'

.PHONY: test-managed-kubernetes-identity-setup
.PHONY: test-managed-kubernetes-port-forward-local
test-managed-kubernetes-port-forward-local:
	@node -e 'const [major,minor]=process.versions.node.split(".").map(Number);if(major<24||(major===24&&minor<17))throw new Error("Node24.17 or newer is required")'
	@timeout 30s node --test tools/dev/owner-session-client.test.mjs tools/dev/owner-runtime-session.test.mjs tools/dev/owner-runtime-https.test.mjs tools/dev/managed-kubernetes-port-forward-codec.test.mjs tools/dev/managed-kubernetes-port-forward-client.test.mjs tools/dev/managed-kubernetes-port-forward-bridge.test.mjs tools/dev/managed-kubernetes-port-forward-cli.test.mjs

.PHONY: test-managed-kubernetes-file-postgres
# Current-source049 owner development; отдельный047 seed не заменяет Bootstrap shipped packages.
test-managed-kubernetes-file-postgres: check-go-toolchain
	@KODEX_TEST_POSTGRES_STORAGE=ram KODEX_CONTROL_PLANE_TEST_GO_PARALLELISM=1 timeout 900s bash scripts/tests/control-plane-postgres-test.sh --managed-kubernetes-file

.PHONY: test-managed-kubernetes-port-forward-postgres
test-managed-kubernetes-port-forward-postgres: check-go-toolchain
	@KODEX_TEST_POSTGRES_STORAGE=ram KODEX_CONTROL_PLANE_TEST_GO_PARALLELISM=1 timeout 900s bash scripts/tests/control-plane-postgres-test.sh --managed-kubernetes-port-forward

.PHONY: test-managed-kubernetes-file-local
test-managed-kubernetes-file-local:
	@node -e 'const [major,minor]=process.versions.node.split(".").map(Number);if(major<24||(major===24&&minor<17))throw new Error("Node24.17 or newer is required")'
	@timeout 60s node --test --test-concurrency=1 tools/dev/owner-session-client.test.mjs tools/dev/owner-runtime-session.test.mjs tools/dev/owner-runtime-https.test.mjs tools/dev/managed-kubernetes-file.test.mjs

.PHONY: test-managed-kubernetes-invocation-postgres
test-managed-kubernetes-invocation-postgres: check-go-toolchain
	@KODEX_TEST_POSTGRES_STORAGE=ram KODEX_CONTROL_PLANE_TEST_GO_PARALLELISM=1 timeout 900s bash scripts/tests/control-plane-postgres-test.sh --managed-kubernetes-invocation

.PHONY: test-managed-kubernetes-capture-postgres
# Ограниченный capture owner current-source049; не полный invocation либо portable upgrade proof.
test-managed-kubernetes-capture-postgres: check-go-toolchain
	@KODEX_TEST_POSTGRES_STORAGE=ram KODEX_CONTROL_PLANE_TEST_GO_PARALLELISM=1 timeout 900s bash scripts/tests/control-plane-postgres-test.sh --managed-kubernetes-capture

.PHONY: test-managed-kubernetes-output-postgres
test-managed-kubernetes-output-postgres: check-go-toolchain
	@KODEX_TEST_POSTGRES_STORAGE=ram KODEX_CONTROL_PLANE_TEST_GO_PARALLELISM=1 timeout 900s bash scripts/tests/control-plane-postgres-test.sh --managed-kubernetes-output

.PHONY: test-managed-kubernetes-gateway
test-managed-kubernetes-gateway: check-go-toolchain
	@cd services/external/integration-gateway && timeout 240s go test -race -count=1 -p=1 -timeout=180s -run '^TestKubernetes' ./internal/app ./internal/integration

.PHONY: test-managed-kubernetes-health-postgres
test-managed-kubernetes-health-postgres: check-go-toolchain
	@KODEX_TEST_POSTGRES_STORAGE=ram KODEX_CONTROL_PLANE_TEST_GO_PARALLELISM=1 timeout 300s bash scripts/tests/control-plane-postgres-test.sh --managed-kubernetes-health

test-managed-kubernetes-identity-setup: check-go-toolchain
	@cd libs/go/serviceruntime && timeout 120s go test -race -p 1 ./... -count=1
	@cd libs/go/httpserver && timeout 120s go test -race -p 1 ./... -count=1
	@cd libs/go/managedkubernetes && timeout 120s go test -race -p 1 ./... -count=1
	@cd services/internal/control-plane && timeout 120s go test -race -p 1 ./internal/domain/service/platform -run '^(TestKubernetes(Setup|Health)|TestManagedKubernetes)' -count=1
	@cd services/internal/control-plane && timeout 120s go test -race -p 1 ./internal/transport/grpc -run '^(TestKubernetesIdentity|TestManagedKubernetesChild)' -count=1
	@cd services/external/control-api-gateway && timeout 120s go test -race -p 1 ./internal/transport/http -run '^TestKubernetesSetupHTTP' -count=1
	@cd services/internal/secret-broker && timeout 180s go test -race -p 1 ./internal/domain/service/kubernetesidentity ./internal/domain/service/kubernetesoperation ./internal/controlplane ./internal/integration/kubernetestarget ./internal/kubernetes ./internal/transport/identityupload ./internal/transport/grpc ./internal/app -count=1
	@cd services/internal/secret-broker && timeout 120s go test -race -p 1 ./cmd/secret-broker -count=1
	@timeout 45s python3 -I -m unittest discover -s tools/dev -p test_managed_kubernetes_identity.py -v

test-contract-registry:
	@timeout 30s node --test scripts/tests/contract-registry-test.mjs

lint-proto: check-proto-toolchain test-contract-registry
	buf lint

.PHONY: test-secret-broker-drafts
.PHONY: test-managed-ssh
test-managed-ssh:
	timeout 240s bash scripts/tests/managed-ssh-test.sh
.PHONY: test-egress-broker-network-policy
.PHONY: test-discovery-provider-fixtures
.PHONY: test-workspace-parent-container
test-workspace-parent-container:
	@timeout 360s bash scripts/tests/workspace-parent-container-test.sh

test-discovery-provider-fixtures:
	@timeout 90s python3 -B scripts/tests/discovery-provider-fixtures-test.py

test-egress-broker-network-policy:
	@timeout 150s python3 -B scripts/tests/egress-broker-network-policy-test.py

test-secret-broker-drafts:
	bash scripts/tests/secret-broker-drafts-test.sh

.PHONY: test-browser-state-component
test-browser-state-component:
	bash scripts/tests/browser-state-component-test.sh

.PHONY: test-automation-scheduler
test-automation-scheduler:
	bash scripts/tests/automation-scheduler-test.sh

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

.PHONY: test-registry-credential-files
test-registry-credential-files:
	@timeout 60s python3 scripts/tests/registry-credential-files-test.py

.PHONY: test-managed-consumer-contract
test-managed-consumer-contract:
	@timeout 40s node scripts/tests/managed-consumer-contract-test.mjs

.PHONY: test-email-mailbox-acceptance
test-email-mailbox-acceptance:
	node --test tools/dev/email-mailbox-acceptance.test.mjs tools/dev/prepare-email-acceptance-input.test.mjs

.PHONY: test-email-mailbox-materialization
test-email-mailbox-materialization:
	GOWORK=off GOMAXPROCS=4 TMPDIR=/tmp go -C services/internal/control-plane test -p 2 -count=1 ./internal/transport/grpc -run '^TestOwnerEmailHelperMaterialization$$'

.PHONY: test-email-managed-claim
test-email-managed-claim: check-go-toolchain
	@./scripts/tests/email-managed-claim-test.sh

.PHONY: test-email-legacy-claim
test-email-legacy-claim:
	./scripts/tests/email-legacy-claim-test.sh

.PHONY: test-email-agent-acceptance
test-email-agent-acceptance:
	TMPDIR=/tmp node --test tools/dev/email-agent-acceptance.test.mjs

.PHONY: test-email-combined-acceptance
test-email-combined-acceptance:
	TMPDIR=/tmp node --test tools/dev/email-agent-acceptance.test.mjs tools/dev/role-image-runtime-proof.test.mjs tools/dev/runtime-workspace-acceptance.test.mjs tools/release/runtime-pod-observe.test.mjs tools/release/authority-executable-readback.test.mjs

.PHONY: test-runner-binary-provenance
test-runner-binary-provenance:
	@timeout 120s python3 -B tools/release/runner-binary-provenance.test.py
	@timeout 60s python3 -B tools/dev/test_build_local_runner_profile.py
	@bash -n tools/dev/build-local-runner.sh
	@./scripts/tests/local-image-cache-import-contract-test.sh

.PHONY: test-role-image-forward-upgrade
test-role-image-forward-upgrade:
	TMPDIR=/tmp node --test tools/dev/role-image-forward-upgrade.test.mjs tools/dev/role-image-acceptance.test.mjs tools/dev/role-image-acceptance-cli.test.mjs tools/dev/role-image-runtime-proof.test.mjs tools/dev/email-agent-acceptance.test.mjs

.PHONY: test-scoped-backup-drill
test-scoped-backup-drill:
	@timeout 60s python3 -m unittest tools/dev/test_scoped_backup_drill.py tools/release/test_backup_release_pin.py
	@bash -n tools/release/pin-backup-controller-release.sh tools/release/render-web-only.sh

.PHONY: test-scoped-backup-reader
.PHONY: test-backup-restore-startup
test-backup-restore-startup:
	@timeout 60s python3 -m unittest discover -s tools/dev -p 'test*backup*.py'

test-scoped-backup-reader:
	@timeout 60s python3 -m unittest discover -s tools/dev -p 'test*backup_reader*.py'
	@bash -n deploy/k8s/base/platform-state/postgresql/reconcile-runtime-credentials.sh
	@./scripts/tests/local-backup-controller-credentials-contract-test.sh

.PHONY: test-control-plane-migration
.PHONY: test-telegram-resource-create test-telegram-notifications-owner test-telegram-authority-delivery

test-telegram-resource-create:
	@timeout 180s node --test tools/release/telegram-runtime-observability.test.mjs tools/release/telegram-resource-create.test.mjs

test-telegram-notifications-owner:
	@timeout 600s bash scripts/tests/control-plane-postgres-test.sh --telegram-notifications-owner

test-control-plane-migration:
	@timeout 1500s node --test tools/release/control-plane-migration.test.mjs tools/release/control-plane-immutable-migration.test.mjs tools/release/portable-component-cohort.test.mjs tools/release/github-repository-scope-proof.test.mjs tools/release/sentry-observability-proof.test.mjs tools/release/telegram-notifications-proof.test.mjs

.PHONY: test-control-api-nats-startup
test-control-api-nats-startup:
	@./scripts/tests/control-api-nats-startup-test.sh

.PHONY: test-staging-capacity-preflight
test-staging-capacity-preflight:
	@PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tools/dev -p test_staging_capacity_preflight.py

.PHONY: test-workflow-result-contract
test-workflow-result-contract:
	@timeout --kill-after=5s 30s python3 scripts/tests/workflow-result-contract-test.py

.PHONY: test-workflow-review-qa-evidence-postgres
test-workflow-review-qa-evidence-postgres: check-go-toolchain
	@timeout --kill-after=10s 360s bash scripts/tests/control-plane-postgres-test.sh '^(TestWorkflowReviewQAEvidenceComponent|TestWorkflowReviewQAEvidenceMigrationProofComponent)$$'

.PHONY: test-workflow-review-qa-upgrade-postgres
test-workflow-review-qa-upgrade-postgres: check-go-toolchain
	@timeout --kill-after=10s 300s bash scripts/tests/control-plane-postgres-test.sh --review-qa-upgrade

.PHONY: test-runtime-checkpoint-predecessor-postgres
test-runtime-checkpoint-predecessor-postgres: check-go-toolchain
	@timeout --kill-after=10s 240s bash scripts/tests/control-plane-postgres-test.sh --checkpoint-predecessor

.PHONY: test-workspace-volume-predecessor-postgres
test-workspace-volume-predecessor-postgres: check-go-toolchain
	@timeout --kill-after=10s 240s bash scripts/tests/control-plane-postgres-test.sh --volume-predecessor

.PHONY: test-workspace-volume-owner-postgres
test-workspace-volume-owner-postgres: check-go-toolchain
	@timeout --kill-after=10s 240s bash scripts/tests/control-plane-postgres-test.sh --volume-owner

.PHONY: test-runtime-workspace-predecessor-postgres
test-runtime-workspace-predecessor-postgres: check-go-toolchain
	@timeout --kill-after=10s 240s bash scripts/tests/control-plane-postgres-test.sh --workspace-predecessor

.PHONY: test-session-workspace-archive-wire
test-session-workspace-archive-wire: check-go-toolchain
	@timeout --kill-after=10s 250s bash scripts/tests/session-workspace-archive-wire-test.sh

.PHONY: test-runtime-workspace-owner-postgres
test-runtime-workspace-owner-postgres: check-go-toolchain
	@timeout --kill-after=10s 240s bash scripts/tests/control-plane-postgres-test.sh '^TestWorkspaceWriterOwnerComponent$$'

.PHONY: test-workflow-workspace-postgres
test-workflow-workspace-postgres: check-go-toolchain
	@timeout --kill-after=10s 240s bash scripts/tests/control-plane-postgres-test.sh --workflow-workspace-upgrade
	@timeout --kill-after=10s 240s bash scripts/tests/control-plane-postgres-test.sh '^(TestWorkflowWorkspaceOwnerComponent|TestWorkspaceWriterOwnerComponent)$$'

.PHONY: test-static-backup-identity-plan
test-static-backup-identity-plan:
	@timeout 30s python3 -B tools/dev/test_static_backup_identity_plan.py

.PHONY: test-static-backup-identity-operator
test-static-backup-identity-operator:
	@timeout 60s python3 -B -m unittest discover -s tools/dev -p 'test_static_backup_identity_*.py'
	@timeout 60s python3 -B tools/dev/test_scoped_backup_drill.py

.PHONY: test-application-restart
test-application-restart:
	@timeout 60s node --test tools/release/application-restart.test.mjs tools/release/scoped-release.test.mjs

.PHONY: test-workspace-restore-owner-postgres
test-workspace-restore-owner-postgres: check-go-toolchain
	@timeout --kill-after=10s 300s bash scripts/tests/control-plane-postgres-test.sh --restore-owner

.PHONY: test-workspace-backup-upgrade-postgres
test-workspace-backup-upgrade-postgres: check-go-toolchain
	@timeout --kill-after=10s 300s bash scripts/tests/control-plane-postgres-test.sh --workspace-backup-upgrade

.PHONY: test-personal-integration-shared-regression-postgres
test-personal-integration-shared-regression-postgres: check-go-toolchain
	@KODEX_TEST_POSTGRES_STORAGE=ram ./scripts/tests/control-plane-postgres-test.sh --personal-credentials-shared-regression

.PHONY: test-personal-integration-invocation-regression-postgres
test-personal-integration-invocation-regression-postgres: check-go-toolchain
	@timeout --kill-after=10s 240s env KODEX_TEST_POSTGRES_STORAGE=ram ./scripts/tests/control-plane-postgres-test.sh --personal-credentials-invocation-regression

.PHONY: test-personal-integration-assistant-regression-postgres
test-personal-integration-assistant-regression-postgres: check-go-toolchain
	@timeout --kill-after=10s 240s env KODEX_TEST_POSTGRES_STORAGE=ram ./scripts/tests/control-plane-postgres-test.sh --personal-credentials-assistant-regression

.PHONY: test-runner-policy-transition
test-runner-policy-transition:
	@timeout 300s node --test tools/release/portable-runner-target.test.mjs tools/release/runner-policy-transition.test.mjs tools/release/default-runner-transition.test.mjs tools/release/component-serving-readback.test.mjs
	@timeout 30s python3 -B tools/install/test_provider_sandbox_profile.py
	@timeout 30s python3 -B tools/install/test_provider_sandbox_transition.py

.PHONY: test-component-publication
test-component-publication:
	@timeout 180s node --test tools/release/component-publication.test.mjs tools/release/stable-image-publication.test.mjs
	@bash -n tools/release/publish-stable-image-worker.sh

.PHONY: test-component-api-maintenance
test-component-api-maintenance:
	@timeout 120s node --test tools/release/component-api-maintenance.test.mjs tools/release/component-api-traffic.test.mjs tools/release/component-api-maintenance-cli.test.mjs tools/release/portable-runner-target.test.mjs

.PHONY: test-component-migration-api-restore
test-component-migration-api-restore:
	@timeout 30s node --test tools/release/component-migration-archive.test.mjs tools/release/component-migration-adoption.test.mjs
	@timeout 420s node --test --test-concurrency=2 tools/release/catalog-authority-baseline.test.mjs tools/release/catalog-authority-baseline.native.test.mjs tools/release/component-migration-api-executor.test.mjs tools/release/component-api-source-equivalence.test.mjs tools/release/component-migration-api-terminal.cli.test.mjs tools/release/migration-owner-database.test.mjs tools/release/component-api-maintenance.test.mjs tools/release/component-api-traffic.test.mjs

.PHONY: test-evicted-pod-metadata-recovery
test-evicted-pod-metadata-recovery:
	@timeout 120s python3 -B tools/release/test_evicted_pod_metadata_recovery.py
	@cd tools/dev && timeout 60s python3 -B -m unittest test_deploy_local_cutover.EvictedProofTest test_deploy_local_cutover.CutoverTest

.PHONY: test-github-repository-scope-upgrade-postgres
test-github-repository-scope-upgrade-postgres: check-go-toolchain
	@python3 tools/release/generate-github-repository-scope-proof.py --check
	@timeout 60s node --test tools/release/github-repository-scope-proof.test.mjs
	@./scripts/tests/control-plane-postgres-test.sh --github-repository-scope-upgrade
	@./scripts/tests/control-plane-postgres-test.sh --github-repository-scope-backup-upgrade

	@./scripts/tests/control-plane-postgres-test.sh --github-repository-scope-public-sequence-negative
	@./scripts/tests/control-plane-postgres-test.sh --github-repository-scope-retained-workspace

.PHONY: test-backup-controller-maintenance
test-backup-controller-maintenance:
	@timeout 360s node --test tools/release/backup-controller-maintenance.test.mjs tools/release/backup-controller-maintenance-cli.test.mjs tools/release/backup-postgres-boundary.test.mjs

# Закрытые107 admission/DELIVER/RETIRE и неизменный106 regression без live effects.
test-telegram-authority-delivery:
	@timeout 180s node --test tools/release/kubernetes-port-names.test.mjs tools/release/telegram-port-repair.test.mjs tools/release/authority-rotation-transition.test.mjs tools/release/authority-api-scope.test.mjs tools/release/telegram-executor-source.test.mjs tools/release/authority-telegram-transition.test.mjs tools/release/portable-component-cohort.test.mjs tools/release/component-publication.test.mjs tools/release/authority-catalog-transition.test.mjs tools/release/authority-catalog-transition.cli.test.mjs tools/release/control-plane-policy-projection.test.mjs tools/release/component-api-traffic.test.mjs tools/release/telegram-resource-create.test.mjs tools/release/telegram-api-readers.test.mjs tools/release/telegram-cp-policy.test.mjs tools/release/telegram-runtime-inputs.test.mjs tools/release/telegram-introduction-inventory.test.mjs

.PHONY: test-telegram-notifications-upgrade-postgres test-telegram-notifications-backup-upgrade-postgres
test-telegram-notifications-upgrade-postgres: check-go-toolchain
	@timeout 600s bash scripts/tests/control-plane-postgres-test.sh --telegram-notifications-upgrade

test-telegram-notifications-backup-upgrade-postgres: check-go-toolchain
	@timeout 600s bash scripts/tests/control-plane-postgres-test.sh --telegram-notifications-backup-upgrade

.PHONY: test-telegram-deployment
test-telegram-deployment:
	@timeout 900s node --test --test-name-pattern='^real CLI046/107:' tools/release/telegram-deployment.cli.test.mjs

.PHONY: test-telegram-frozen-delivery
test-telegram-frozen-delivery:
	@timeout 900s node --test --test-name-pattern='^real CLI046/107 frozen:' tools/release/telegram-deployment.cli.test.mjs

.PHONY: test-telegram-port-repair-backup
test-telegram-port-repair-backup:
	@timeout 60s node --test tools/release/telegram-backup-inventory.test.mjs
	@timeout 900s node --test --test-name-pattern='^real CLI046/107 backup-port-repair:' tools/release/telegram-deployment.cli.test.mjs

.PHONY: test-telegram-port-repair
test-telegram-port-repair:
	@timeout 90s node --test tools/release/kubernetes-port-names.test.mjs tools/release/telegram-port-repair.test.mjs
	@timeout 900s node --test --test-name-pattern='^real CLI046/107 (port-repair|invalid-port):' tools/release/telegram-deployment.cli.test.mjs

.PHONY: test-telegram-native-egress
test-telegram-native-egress: check-go-toolchain
	@timeout 360s bash scripts/tests/telegram-native-egress-test.sh

.PHONY: test-ssh-identity-setup
test-ssh-identity-setup: check-go-toolchain
	@cd libs/go/managedssh && timeout 60s go test -race -count=1 -p=1 -timeout=45s ./...
	@cd services/internal/control-plane && timeout 90s go test -race -count=1 -p=1 -timeout=60s -run '^TestSSH(Setup|Identity)' ./internal/domain/service/platform ./internal/transport/grpc
	@cd services/internal/secret-broker && timeout 90s go test -race -count=1 -p=1 -timeout=60s ./internal/domain/service/sshidentity ./internal/kubernetes ./internal/integration/sshtarget ./internal/controlplane ./internal/transport/http

.PHONY: test-ssh-upload-app
test-ssh-upload-app: check-go-toolchain
	@cd libs/go/serviceruntime && timeout 120s go test -race -count=1 -p=1 -timeout=90s ./...
	@cd libs/go/httpserver && timeout 90s go test -race -count=1 -p=1 -timeout=60s ./...
	@cd services/internal/secret-broker && timeout 240s go test -race -count=1 -p=1 -timeout=90s -run '^Test(BrokerRPC|SSHUpload|SSHIdentityStorage|SSHOwnerReadiness|MainFails)' ./internal/app ./internal/kubernetes ./internal/controlplane ./cmd/secret-broker

.PHONY: test-ssh-setup-owner-unit
test-ssh-setup-owner-unit: check-go-toolchain
	@cd services/internal/control-plane && timeout 180s go test -race -count=1 -p=1 -timeout=60s -run '^Test(SSH(OwnerInfrastructure|UploadRecovery|CompletionOutcome|PrivateOwner)|AccessInfrastructureClassification)' ./internal/repository/postgres/platform

.PHONY: test-ssh-health-owner-unit
test-ssh-health-owner-unit: check-go-toolchain
	@cd libs/go/integrationpackage && timeout 180s go test -race -count=1 -p=1 -timeout=60s -run '^TestSSHPermission' ./...
	@cd services/internal/control-plane && timeout 180s go test -race -count=1 -p=1 -timeout=60s -run '^Test(SSH(IdentityHealth|Health)|PersonalBroker|PersonalCredential.*Claim)' ./internal/repository/postgres/platform ./internal/domain/service/platform ./internal/transport/grpc
	@cd services/internal/secret-broker && timeout 180s go test -race -count=1 -p=1 -timeout=60s -run '^TestSSHHealth' ./internal/domain/service/sshidentity ./internal/integration/sshtarget ./internal/controlplane ./internal/transport/grpc ./internal/app

.PHONY: test-shared-integration-health-unit
test-shared-integration-health-unit: check-go-toolchain
	@cd services/internal/control-plane && timeout 300s go test -race -count=1 -p=1 -timeout=60s -run '^Test(SharedHealth|PersonalBrokerIdentityClaimMetadata)' ./internal/repository/postgres/platform ./internal/transport/grpc

.PHONY: test-broker-identity-claim-metadata
test-broker-identity-claim-metadata: check-go-toolchain
	@cd services/internal/control-plane && timeout 300s go test -race -count=1 -p=1 -timeout=60s -run '^Test(InvocationClaimCredentialDelivery|ClaimUnknownPurpose|PersonalBrokerIdentityClaimMetadata)' ./internal/transport/grpc

.PHONY: test-ssh-operation-input
test-ssh-operation-input: check-go-toolchain
	@cd libs/go/managedssh && timeout 120s go test -race -count=1 -p=1 -timeout=60s ./...
	@cd libs/go/integrationpackage && timeout 120s go test -race -count=1 -p=1 -timeout=60s -run '^TestSSH(Input|Definition|Null)' ./...
	@cd services/internal/secret-broker && timeout 120s go test -race -count=1 -p=1 -timeout=60s -run '^Test(Target|Run|Host|Failure|Managed|Operator|Execute|Start|Output|Cancel)' ./internal/integration/sshtarget

.PHONY: test-ssh-purpose-execution
test-ssh-purpose-execution: check-go-toolchain
	@cd libs/go/managedssh && timeout 150s go test -race -count=1 -p=1 -timeout=60s ./...
	@cd services/internal/secret-broker && timeout 180s go test -race -count=1 -p=1 -timeout=60s -run '^Test(SSHExecution|SSHHealth|Managed|Execute|Operator)' ./internal/integration/sshtarget

.PHONY: test-ssh-setup-api-unit
test-ssh-setup-api-unit: check-go-toolchain
	@cd services/external/control-api-gateway && timeout 180s go test -race -count=1 -p=1 -timeout=60s -run '^TestSSHSetupHTTP' ./internal/transport/http

.PHONY: test-ssh-setup-ui-unit
test-ssh-setup-ui-unit:
	@cd services/staff/control-center && timeout 90s npm run test:unit -- src/features/integrations/ssh-identity-setup.test.ts

.PHONY: test-ssh-identity-browser
test-ssh-identity-browser:
	@cd services/staff/control-center && timeout 180s npm run build:synthetic
	@cd services/staff/control-center && timeout 180s npx playwright test --config playwright.synthetic.config.ts e2e/ssh-identity-setup.synthetic.spec.ts e2e/personal-connections.synthetic.spec.ts --grep SSH --global-timeout 120000

.PHONY: test-ssh-identity-render
test-ssh-identity-render:
	@timeout 150s python3 -B scripts/tests/ssh-identities-render-test.py
	@cd tools/install && timeout 30s python3 -B -m unittest test_ensure_namespace
	@cd tools/dev && timeout 30s python3 -B -m unittest test_installation_inventory test_installation_cleanup

.PHONY: test-ssh-operator-render
test-ssh-operator-render: check-go-toolchain
	@cd tools/managed-ssh-helper && KODEX_SSH_OPERATOR_KUSTOMIZE=1 timeout 120s go test -race -count=1 -p=1 ./internal/operator ./cmd/ssh-qualification
	@timeout 180s python3 -B tools/install/test_ssh_qualification_profile.py

.PHONY: test-personal-integrations-upgrade
test-personal-integrations-upgrade: check-go-toolchain
	@python3 -B tools/release/generate-personal-integrations-proof.py --check
	@KODEX_TEST_POSTGRES_STORAGE=ram KODEX_CONTROL_PLANE_TEST_GO_PARALLELISM=1 timeout 420s bash scripts/tests/control-plane-postgres-test.sh --personal-integrations-upgrade
	@KODEX_TEST_POSTGRES_STORAGE=ram KODEX_CONTROL_PLANE_TEST_GO_PARALLELISM=1 timeout 420s bash scripts/tests/control-plane-postgres-test.sh --personal-integrations-backup-upgrade

.PHONY: test-telegram-authority-owner-native
test-telegram-authority-owner-native:
	@timeout 360s node tools/release/telegram-authority-owner.native.mjs

.PHONY: test-telegram-authority-owner
test-telegram-authority-owner:
	@timeout 150s node --test tools/release/telegram-authority-owner.test.mjs tools/release/authority-catalog-transition.cli.test.mjs tools/release/telegram-port-repair.test.mjs tools/release/telegram-executor-source.test.mjs

.PHONY: test-telegram-reader-bootstrap
test-telegram-reader-bootstrap:
	@timeout 1800s node --test --test-name-pattern='^real CLI046/107 reader-bootstrap:' tools/release/telegram-deployment.cli.test.mjs

.PHONY: test-authority-distribute-status-native
test-authority-distribute-status-native:
	@timeout 360s node tools/release/telegram-authority-owner.native.mjs --distribute-status

.PHONY: test-artifact-retention-issuer-migration-local
test-artifact-retention-issuer-migration-local:
	@timeout 60s node --test tools/release/artifact-retention-issuer-migration.test.mjs tools/release/migration-owner-database.test.mjs

.PHONY: test-artifact-retention-issuer-assets-local
test-artifact-retention-issuer-assets-local:
	@timeout 90s node --test tools/release/artifact-retention-issuer-assets.test.mjs tools/release/artifact-retention-issuer-inputs.test.mjs

.PHONY: test-authority-distribute-status-migration
test-authority-distribute-status-migration:
	@timeout 90s node --test tools/release/authority-distribute-status-migration.test.mjs

.PHONY: test-telegram-observability-repair
test-telegram-observability-repair:
	@timeout 300s node --test tools/release/telegram-runtime-observability.test.mjs tools/release/telegram-observability-repair.test.mjs tools/release/portable-component-cohort.test.mjs tools/release/backup-controller-maintenance-cli.test.mjs

.PHONY: test-telegram-runtime-inputs
test-telegram-runtime-inputs:
	@timeout 120s node --test tools/release/telegram-runtime-inputs.test.mjs
	@timeout 120s node --test --test-name-pattern='runtime-metadata:' tools/release/telegram-deployment.cli.test.mjs

.PHONY: test-telegram-egress-revision-repair
test-telegram-egress-revision-repair:
	@timeout 300s node --test tools/release/telegram-egress-revision-repair.test.mjs tools/release/component-node-qualification.test.mjs tools/release/portable-component-cohort.test.mjs tools/release/backup-controller-maintenance-cli.test.mjs

.PHONY: test-ssh-setup-revoke-development
test-ssh-setup-revoke-development: check-go-toolchain
	@timeout 300s bash scripts/tests/ssh-identity-owner-development.sh --revoke

.PHONY: test-ssh-execution-current-entrypoint
test-ssh-execution-current-entrypoint:
	@timeout 30s python3 -B scripts/tests/ssh-current-owner-entrypoint-test.py

.PHONY: test-ssh-execution-current-postgres
test-ssh-execution-current-postgres: check-go-toolchain
	@timeout --kill-after=10s 300s env KODEX_TEST_POSTGRES_STORAGE=ram bash scripts/tests/control-plane-postgres-test.sh --ssh-identity-owner-current

.PHONY: test-ssh-setup-owner-development
test-ssh-setup-owner-development: check-go-toolchain
	@KODEX_TEST_POSTGRES_STORAGE=ram KODEX_CONTROL_PLANE_TEST_GO_PARALLELISM=1 timeout 300s bash scripts/tests/ssh-identity-owner-development.sh

.PHONY: test-integration-pending-root-postgres
test-integration-pending-root-postgres: check-go-toolchain
	@KODEX_TEST_POSTGRES_STORAGE=ram KODEX_CONTROL_PLANE_TEST_GO_PARALLELISM=1 timeout 300s bash scripts/tests/control-plane-postgres-test.sh --integration-pending-root-owner-regression

.PHONY: test-ssh-pending-root-development
test-ssh-pending-root-development: check-go-toolchain
	@KODEX_TEST_POSTGRES_STORAGE=ram KODEX_CONTROL_PLANE_TEST_GO_PARALLELISM=1 timeout 300s bash scripts/tests/control-plane-postgres-test.sh --ssh-integration-pending-root-owner-development

.PHONY: test-ssh-receipt-scope-development
test-ssh-receipt-scope-development: check-go-toolchain
	@KODEX_TEST_POSTGRES_STORAGE=ram KODEX_CONTROL_PLANE_TEST_GO_PARALLELISM=1 timeout 300s bash scripts/tests/control-plane-postgres-test.sh --ssh-receipt-scope-owner-development

.PHONY: test-ssh-execution-deadline-development
test-ssh-execution-deadline-development: check-go-toolchain
	@KODEX_TEST_POSTGRES_STORAGE=ram KODEX_CONTROL_PLANE_TEST_GO_PARALLELISM=1 timeout 300s bash scripts/tests/control-plane-postgres-test.sh --ssh-execution-deadline-owner-development

.PHONY: test-shared-integration-health-development
test-shared-integration-health-development: check-go-toolchain
	@KODEX_TEST_POSTGRES_STORAGE=ram KODEX_CONTROL_PLANE_TEST_GO_PARALLELISM=1 timeout 300s bash scripts/tests/ssh-identity-owner-development.sh --shared-health

.PHONY: test-ssh-shared-credential-sdk
test-ssh-shared-credential-sdk: check-go-toolchain
	@cd libs/go/internalrpcauth && timeout 120s go test -race -count=1 -p=1 -timeout=60s -run ExactJSON .
	@cd libs/go/managedkubernetes && timeout 240s go test -race -count=1 -p=1 -timeout=120s ./...
	@cd libs/go/integrationpackage && timeout 360s go test -race -count=1 -p=1 -timeout=240s ./...
	@timeout 180s $(MAKE) check-integration-package-codegen

.PHONY: test-ssh-invocation-use-policy
test-ssh-invocation-use-policy: check-go-toolchain
	@cd services/internal/control-plane && timeout 240s go test -race -count=1 -p=1 -timeout=90s -run '^Test(PurposeInvocationUsePolicy|IntegrationCapabilityAuthority|EffectiveCapability|SSH)' ./internal/repository/postgres/platform

# Source/transport профиль не подтверждает canonical048/109 или installed SSH.
.PHONY: test-ssh-execution-owner-source
test-ssh-execution-owner-source: check-go-toolchain
	@cd libs/go/managedssh && timeout 180s go test -race -count=1 -p=1 -timeout=120s ./...
	@cd services/internal/control-plane && timeout 300s go test -race -count=1 -p=1 -timeout=120s -run '^TestSSH(Execution|HealthBinding|HealthCompletion)' ./internal/repository/postgres/platform ./internal/domain/service/platform ./internal/transport/grpc
	@cd services/internal/secret-broker && timeout 300s go test -race -count=1 -p=1 -timeout=120s -run '^TestSSH(Execution|IdentityExecution|HealthComposition)' ./internal/domain/service/sshidentity ./internal/controlplane ./internal/integration/sshtarget ./internal/transport/grpc ./internal/app
	@cd services/external/integration-gateway && timeout 300s go test -race -count=1 -p=1 -timeout=120s -run '^TestSSH(Invocation|Health|ConsumerFactory|BrokerProfile)|^TestBrokerIdentity|^TestKubernetesHealthOnly' ./internal/integration ./internal/app

# Настоящий helper filesystem/receipt process через encrypted SSH; не owner048/109 proof.
.PHONY: test-ssh-native-helper-process
.PHONY: test-ssh-identity-upload-profile
test-ssh-identity-upload-profile:
	@timeout 90s python3 -B tools/install/test_ssh_identity_profile.py

test-ssh-native-helper-process: check-go-toolchain
	@timeout 240s bash scripts/tests/ssh-native-helper-process.sh
.PHONY: test-managed-kubernetes-material-public
test-managed-kubernetes-material-public: check-go-toolchain
	@cd services/internal/control-plane && timeout 180s go test -race -p 1 -timeout 120s ./internal/domain/service/platform ./internal/transport/grpc -run '^TestKubernetesResourceMaterial' -count=1
	@cd services/external/control-api-gateway && timeout 180s go test -race -p 1 -timeout 120s ./internal/transport/http -run '^TestMaterialHTTP|^TestKubernetesSetupHTTP' -count=1
	@timeout 60s python3 -I -B -m unittest discover -s tools/dev -p 'test_managed_kubernetes*identity.py' -v
	@timeout 60s python3 -I -B -m unittest discover -s tools/dev -p test_managed_kubernetes_resource_material.py -v

.PHONY: test-managed-kubernetes-identity-export
test-managed-kubernetes-identity-export: check-go-toolchain
	@test -n "$${KODEX_TEST_KUBECTL_BINARY:-}" || { echo "KODEX_TEST_KUBECTL_BINARY is required" >&2; exit 1; }
	@timeout 90s python3 -I -B -m unittest discover -s tools/dev -p test_managed_kubernetes_identity_export.py -v
	@cd services/internal/secret-broker && timeout 180s go test -race -p 1 -timeout 120s ./internal/integration/kubernetestarget -run '^TestKubernetesIdentityExportProcess' -count=1

.PHONY: test-managed-kubernetes-health-readiness
test-managed-kubernetes-health-readiness: check-go-toolchain
	@cd services/internal/control-plane && timeout 240s go test -race -count=1 -p=1 -timeout=90s -run '^Test(KubernetesIdentityHealthReadiness|ManagedKubernetesIdentityHealthProfile|KubernetesDeliveryContract|ManagedKubernetesOutputContract)' ./internal/domain/service/platform ./internal/transport/grpc
	@cd services/internal/secret-broker && timeout 240s go test -race -count=1 -p=1 -timeout=90s -run '^TestKubernetesIdentityStartupUsesCoreHealthReadiness$$' ./internal/app

.PHONY: test-managed-kubernetes-effect-acl-postgres
test-managed-kubernetes-effect-acl-postgres: check-go-toolchain
	@KODEX_TEST_POSTGRES_STORAGE=ram KODEX_CONTROL_PLANE_TEST_GO_PARALLELISM=1 timeout 300s bash scripts/tests/control-plane-postgres-test.sh --managed-kubernetes-effect-acl

.PHONY: test-managed-kubernetes-effect-acl-development
test-managed-kubernetes-effect-acl-development: check-go-toolchain
	@KODEX_TEST_POSTGRES_STORAGE=ram KODEX_CONTROL_PLANE_TEST_GO_PARALLELISM=1 timeout 300s bash scripts/tests/control-plane-postgres-test.sh --kubernetes-effect-acl-owner-development

.PHONY: test-personal-integrations-build-inputs-local
test-personal-integrations-build-inputs-local:
	@timeout 15s node --test tools/release/personal-integrations-build-inputs.test.mjs tools/release/frontend-build-context.test.mjs

.PHONY: test-artifact-retention-issuer-historical-readiness
test-artifact-retention-issuer-historical-readiness:
	@timeout --kill-after=5s 60s node --test --test-concurrency=1 --test-timeout=45000 tools/release/artifact-retention-issuer-historical-readiness.test.mjs tools/release/artifact-retention-issuer-migration.test.mjs tools/release/migration-owner-database.test.mjs

.PHONY: test-personal-integrations-reader-scope
test-personal-integrations-reader-scope:
	@timeout --kill-after=5s 180s node --test --test-concurrency=1 tools/release/personal-integrations-executor-source.test.mjs tools/release/personal-integrations-artifact-spec.test.mjs tools/release/personal-integrations-reader-scope.test.mjs
	@timeout --kill-after=5s 70s node --test --test-name-pattern="^049:" tools/release/portable-component-cohort.test.mjs

.PHONY: test-paired-token-release-plan-local
test-paired-token-release-plan-local:
	@timeout --kill-after=5s 60s node --test --test-concurrency=1 tools/release/paired-token-release-plan.test.mjs tools/release/paired-token-release-cli.test.mjs tools/release/migration-owner-database.test.mjs

.PHONY: test-paired-token-release-receiver-local
test-paired-token-release-receiver-local:
	@timeout --kill-after=5s 60s node --test --test-concurrency=1 tools/release/paired-token-release-receiver.test.mjs tools/release/paired-token-release-plan.test.mjs tools/release/paired-token-release-cli.test.mjs tools/release/migration-owner-database.test.mjs

.PHONY: test-paired-token-native-join-local
test-paired-token-native-join-local:
	@timeout --kill-after=5s 90s node --test --test-concurrency=1 tools/release/paired-token-native-join.test.mjs tools/release/paired-token-release-receiver.test.mjs tools/release/paired-token-release-plan.test.mjs tools/release/paired-token-release-cli.test.mjs tools/release/migration-owner-database.test.mjs

.PHONY: test-paired-token-terminal-local
test-paired-token-terminal-local:
	@timeout --kill-after=5s 120s node --test --test-concurrency=1 tools/release/paired-token-release-terminal.test.mjs tools/release/paired-token-native-join.test.mjs tools/release/paired-token-release-receiver.test.mjs tools/release/paired-token-release-plan.test.mjs tools/release/paired-token-release-cli.test.mjs tools/release/migration-owner-database.test.mjs

.PHONY: test-multi-component-serving-inventory-local
test-multi-component-serving-inventory-local:
	@timeout --kill-after=5s 60s node --test --test-concurrency=1 --test-name-pattern='multi-component:|metadata-first serving read|metadata/full-object races|serving inventory исключает|canonical backup terminal command|registered044/045/046 terminal commands' tools/release/multi-component-serving-inventory.test.mjs tools/release/control-plane-immutable-migration.test.mjs

.PHONY: test-paired-token-owner-batch-local
test-paired-token-owner-batch-local:
	@timeout --kill-after=5s 120s node --test --test-concurrency=1 tools/release/paired-authority-publisher-read.test.mjs tools/release/paired-token-owner-database.test.mjs tools/release/paired-token-release-owner.test.mjs tools/release/catalog-authority-baseline.test.mjs tools/release/migration-owner-database.test.mjs tools/release/paired-token-release-plan.test.mjs tools/release/paired-token-release-cli.test.mjs
