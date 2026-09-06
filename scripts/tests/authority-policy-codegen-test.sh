#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Authority policy codegen test failed: %s\n' "$*" >&2
  exit 1
}

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT
generated="$temporary_directory/authority-policy.json"
canonical="$repository_root/deploy/k8s/base/internal-rpc-authority-publisher/authority-policy.json"

(
  cd -- "$repository_root/libs/go/controlplaneclient"
  env -u GOFLAGS GOENV=off GOWORK=off go run ./cmd/policygen \
    --output "$generated" \
    --oidc-issuer '__KODEX_OIDC_ISSUER__' \
    --oidc-audience kodex-control-api
)

cmp -s "$generated" "$canonical" || fail 'generated policy differs from the canonical file'
jq -e '
  def provider_operations: [
    "platform.provider-accounts.model-catalog.observe",
    "platform.provider-credentials.api-key.materialize",
    "platform.provider-credentials.cleanup",
    "platform.provider-credentials.device-authorize.get",
    "platform.provider-credentials.device-authorize.start",
    "platform.provider-credentials.materialization.discard",
    "platform.provider-credentials.readiness.check"
  ];
  .v == 1 and .policy.default_decision == "DENY" and
	.policy_revision == 73 and .policy.authority_abi_version == 2 and
  ([.policy.operation_bindings[] | select(.operation_id == "platform.query.provider-accounts.blockers.list") |
    select(.caller_workload_id == "control-api-gateway" and .target_workload_id == "control-plane" and
      .request_profile == {"mode":"UNARY_PROTO_SHA256","resource":"REQUIRED","version":"FORBIDDEN","attempt":"FORBIDDEN","idempotency":"FORBIDDEN"})] | length) == 1 and
  ([.policy.operation_bindings[] | select(.operation_id == "platform.command.provider-accounts.queued-work.cancel") |
    select(.caller_workload_id == "control-api-gateway" and .target_workload_id == "control-plane" and
      .request_profile == {"mode":"UNARY_PROTO_SHA256","resource":"REQUIRED","version":"REQUIRED","attempt":"FORBIDDEN","idempotency":"REQUIRED"})] | length) == 1 and
  ([.policy.operation_bindings[] | select(.operation_id == "platform.runtime.execution.artifact.stream") |
    select(.caller_workload_id == "runtime-controller" and .target_workload_id == "control-plane" and .project_required == false and
      .request_profile == {"mode":"UNARY_PROTO_SHA256","resource":"FORBIDDEN","version":"FORBIDDEN","attempt":"FORBIDDEN","idempotency":"FORBIDDEN"})] | length) == 1 and
  ([.policy.operation_bindings[] | select(.operation_id | startswith("platform.runtime.files.")) |
    select(.caller_workload_id == "runtime-controller" and .target_workload_id == "control-plane" and .project_required == false and
      .request_profile == {"mode":"UNARY_PROTO_SHA256","resource":"FORBIDDEN","version":"FORBIDDEN","attempt":"FORBIDDEN","idempotency":"FORBIDDEN"})] | length) == 4 and
  ([.policy.operation_bindings[] | select(.operation_id == "platform.query.artifact-binding-targets.list" or .operation_id == "platform.query.run-attachment-eligibility.get") |
    select(.caller_workload_id == "control-api-gateway" and .target_workload_id == "control-plane" and
      .authority_proof_producer_id == "control-plane.oidc" and .project_required == false and
      .request_profile == {"mode":"UNARY_PROTO_SHA256","resource":"REQUIRED","version":"FORBIDDEN","attempt":"FORBIDDEN","idempotency":"FORBIDDEN"})] | length) == 2 and
  ([.policy.operation_bindings[] | select(.operation_id | startswith("platform.configuration-writebacks.work.")) |
    select(.caller_workload_id == "integration-gateway" and .target_workload_id == "control-plane" and .project_required == false and
      .request_profile == {"mode":"UNARY_PROTO_SHA256","resource":"FORBIDDEN","version":"FORBIDDEN","attempt":"FORBIDDEN","idempotency":"FORBIDDEN"})] | length) == 5 and
  ([.policy.operation_bindings[] | select(.operation_id == "platform.query.configuration-writebacks.get" or .operation_id == "platform.query.configuration-writebacks.list") |
    select(.request_profile == {"mode":"UNARY_PROTO_SHA256","resource":"REQUIRED","version":"FORBIDDEN","attempt":"FORBIDDEN","idempotency":"FORBIDDEN"} and .project_required == false)] | length) == 2 and
  ([.policy.operation_bindings[] | select(.operation_id == "platform.command.role-image-writebacks.prepare" or .operation_id == "platform.command.integration-definition-writebacks.prepare" or .operation_id == "platform.command.configuration-writebacks.approve" or .operation_id == "platform.command.configuration-writebacks.reject" or .operation_id == "platform.command.configuration-writebacks.cancel") |
    select(.request_profile == {"mode":"UNARY_PROTO_SHA256","resource":"REQUIRED","version":"REQUIRED","attempt":"FORBIDDEN","idempotency":"REQUIRED"} and .project_required == false)] | length) == 5 and
  ([.policy.operation_bindings[] | select(.operation_id == "platform.query.agent-effective-capabilities.get" and
    .full_method == "/controlplane.v1.PlatformQueryService/GetAgentEffectiveCapabilities" and
    .request_profile == {"mode":"UNARY_PROTO_SHA256","resource":"REQUIRED","version":"FORBIDDEN","attempt":"FORBIDDEN","idempotency":"FORBIDDEN"} and .project_required == false)] | length) == 1 and
  ([.policy.operation_bindings[] | select(.operation_id == "platform.query.config-overlays.revisions.list" or .operation_id == "platform.query.config-overlays.revisions.get") |
    select(.request_profile == {"mode":"UNARY_PROTO_SHA256","resource":"REQUIRED","version":"FORBIDDEN","attempt":"FORBIDDEN","idempotency":"FORBIDDEN"} and .project_required == false)] | length) == 2 and
	(.policy.authority_proof_producers | length) == 15 and
  ([.policy.operation_bindings[] | select(.caller_workload_id == "email-bridge") | .operation_id] | sort) ==
    ["platform.email.authorization.resolve", "platform.email.configuration.report", "platform.email.effect-receipts.report", "platform.email.reconciliation.resolve"] and
  all(.policy.operation_bindings[] | select(.caller_workload_id == "email-bridge");
    .authority_proof_producer_id == "control-plane.email-bridge" and
    .target_workload_id == "control-plane" and .project_required == false and
    .authority_sources == ["DOMAIN_STATE"] and
    .caller_spiffe_id == "spiffe://kodex.local/ns/kodex-system/sa/email-bridge") and
  ([.policy.authority_proof_producers[] | select(.producer_id == "control-plane.email-bridge" and
    .caller_workload_id == "email-bridge" and .owner_workload_id == "control-plane" and
    .application_credential == "PLATFORM_WORKER_GRANT" and
    .application_credential_audience == "urn:kodex:platform-worker:email-bridge" and
    .application_credential_trust_bundle_id == "email-bridge-platform-worker-grants-g1" and
    .allowed_operation_ids == ["platform.email.authorization.resolve", "platform.email.configuration.report", "platform.email.effect-receipts.report", "platform.email.reconciliation.resolve"])] | length) == 1 and
  ([.policy.operation_bindings[] | select(.operation_id == "platform.email.effect-receipts.report" and
    .request_profile == {"mode":"UNARY_PROTO_SHA256","resource":"FORBIDDEN","version":"FORBIDDEN","attempt":"FORBIDDEN","idempotency":"REQUIRED"})] | length) == 1 and
  ([.policy.operation_bindings[] | select(.operation_id == "platform.email.configuration.report" and
    .request_profile == {"mode":"UNARY_PROTO_SHA256","resource":"FORBIDDEN","version":"FORBIDDEN","attempt":"FORBIDDEN","idempotency":"FORBIDDEN"})] | length) == 1 and
  ([.policy.operation_bindings[] | select(.operation_id == "platform.command.email-mailbox.drafts.create" and
    .request_profile == {"mode":"UNARY_PROTO_SHA256","resource":"FORBIDDEN","version":"FORBIDDEN","attempt":"FORBIDDEN","idempotency":"REQUIRED"})] | length) == 1 and
  ([.policy.operation_bindings[] | select(.operation_id == "platform.command.email-mailbox.configurations.bind" and
    .request_profile == {"mode":"UNARY_PROTO_SHA256","resource":"REQUIRED","version":"REQUIRED","attempt":"FORBIDDEN","idempotency":"REQUIRED"})] | length) == 1 and
  ([.policy.operation_bindings[] | select(.operation_id == "platform.command.email-effects.reconcile" and
    .request_profile == {"mode":"UNARY_PROTO_SHA256","resource":"REQUIRED","version":"REQUIRED","attempt":"FORBIDDEN","idempotency":"REQUIRED"})] | length) == 1 and
  ((.policy.operation_bindings | map(.operation_id) | unique | length) ==
   (.policy.operation_bindings | length)) and
  all(.policy.operation_bindings[];
    .permission != "" and .full_method != "" and
    ((has("continuation") and (has("authority_proof_producer_id") | not)) or
      ((has("continuation") | not) and .authority_proof_producer_id != "")) and
    (.request_profile.mode == "UNARY_PROTO_SHA256" or .request_profile.mode == "STREAM_SESSION")) and
  ([.policy.operation_bindings[] |
		select(.authority_proof_producer_id == "secret-broker.provider-credential-materializer") | .operation_id] | sort) == provider_operations and
  ([.policy.operation_bindings[] | select(.operation_id == "platform.provider-accounts.model-catalog.observe" and
    .permission == "platform.provider-accounts.model-catalog.observe" and
    .full_method == "/controlplane.v1.ProviderCredentialMaterializerService/ObserveProviderModelCatalog" and
    .project_required == false and .authority_sources == ["DOMAIN_STATE"] and
    .request_profile == {"mode":"UNARY_PROTO_SHA256","resource":"FORBIDDEN","version":"FORBIDDEN","attempt":"FORBIDDEN","idempotency":"FORBIDDEN"})] | length) == 1 and
	all(.policy.operation_bindings[] | select(.authority_proof_producer_id == "secret-broker.provider-credential-materializer");
		.caller_workload_id == "control-plane" and
		.caller_spiffe_id == "spiffe://kodex.local/ns/kodex-system/sa/control-plane" and
    .target_spiffe_id == "spiffe://kodex.local/ns/kodex-system/sa/secret-broker" and
    .audience == "urn:kodex:internal-rpc:secret-broker" and
    .target_tls_server_name == "secret-broker.kodex-system.svc.cluster.local") and
  ([.policy.operation_bindings[] | select(.caller_workload_id == "runtime-controller" and .target_workload_id == "secret-broker") | .operation_id] | sort) ==
    ["platform.runtime.credentials.materialize", "platform.runtime.credentials.readiness.check", "platform.runtime.credentials.system-assistant.materialize"] and
  all(.policy.operation_bindings[] | select(.caller_workload_id == "runtime-controller" and .target_workload_id == "secret-broker");
    .project_required == (.operation_id != "platform.runtime.credentials.system-assistant.materialize") and .authority_sources == ["DOMAIN_STATE", "OIDC_SESSION", "RUNTIME_EXECUTION"]) and
  ([.policy.operation_bindings[] | select(.caller_workload_id == "stt-tts-service" and .target_workload_id == "secret-broker") | .operation_id]) ==
    ["platform.stt.credential.project"] and
  all(.policy.operation_bindings[] | select(.caller_workload_id == "stt-tts-service" and .target_workload_id == "secret-broker");
    .project_required == false and .authority_sources == ["DOMAIN_STATE", "OIDC_SESSION", "RUNTIME_EXECUTION"]) and
  ([.policy.operation_bindings[] |
    select(.operation_id == "platform.provider-credentials.cleanup" and
      .permission == "platform.provider-credentials.cleanup" and
      .full_method == "/controlplane.v1.ProviderCredentialMaterializerService/CleanupProviderCredential" and
      .caller_workload_id == "control-plane" and
      .target_workload_id == "secret-broker" and
      .authority_proof_producer_id == "secret-broker.provider-credential-materializer" and
      .authority_sources == ["DOMAIN_STATE"] and
      .project_required == false)] | length) == 1 and
  ([.policy.operation_bindings[] | select(.operation_id == "platform.stt.model-catalog.get" and
      .caller_workload_id == "control-api-gateway" and .target_workload_id == "stt-tts-service" and
      .authority_proof_producer_id == "control-plane.oidc-stt" and
      .full_method == "/stt.v1.SpeechToTextService/GetModelCatalog" and .project_required == false and .permission == "system.configuration.manage" and
      .request_profile == {"mode":"UNARY_PROTO_SHA256","resource":"FORBIDDEN","version":"FORBIDDEN","attempt":"FORBIDDEN","idempotency":"FORBIDDEN"})] | length) == 1 and
  ([.policy.operation_bindings[] | select(.operation_id == "platform.stt.transcribe" and
      .caller_workload_id == "control-api-gateway" and .target_workload_id == "stt-tts-service" and
      .full_method == "/stt.v1.SpeechToTextService/Transcribe" and .project_required == false and .permission == "stt.transcribe" and
      .request_profile == {"mode":"STREAM_SESSION","resource":"FORBIDDEN","version":"FORBIDDEN","attempt":"FORBIDDEN","idempotency":"REQUIRED"})] | length) == 1 and
	all(.policy.operation_bindings[] | select(.operation_id == "platform.stt.policy.resolve" or .operation_id == "platform.stt.credential.project");
		.caller_workload_id == "stt-tts-service" and .project_required == false and
		.authority_sources == ["DOMAIN_STATE", "OIDC_SESSION", "RUNTIME_EXECUTION"] and
    (has("authority_proof_producer_id") | not) and
    .continuation.parent_operation_id == "platform.stt.transcribe" and
    .continuation.parent_full_method == "/stt.v1.SpeechToTextService/Transcribe" and
    .request_profile == {"mode":"UNARY_PROTO_SHA256","resource":"REQUIRED","version":"REQUIRED","attempt":"FORBIDDEN","idempotency":"REQUIRED"}) and
  all(.policy.operation_bindings[] | select(.operation_id ==
      "platform.command.provider-accounts.device-authorize" or .operation_id ==
      "platform.command.provider-accounts.device-verify" or .operation_id ==
      "platform.command.provider-accounts.device-reauthorize");
    .request_profile == {"mode":"UNARY_PROTO_SHA256","resource":"REQUIRED","version":"REQUIRED","attempt":"FORBIDDEN","idempotency":"REQUIRED"}) and
  ([.policy.operation_bindings[] | select(.operation_id == "platform.provider-credentials.device-authorize.start" and
    .request_profile == {"mode":"UNARY_PROTO_SHA256","resource":"REQUIRED","version":"FORBIDDEN","attempt":"REQUIRED","idempotency":"REQUIRED"})] | length) == 1 and
  ([.policy.operation_bindings[] | select(.operation_id == "platform.provider-credentials.device-authorize.get" and
    .request_profile == {"mode":"UNARY_PROTO_SHA256","resource":"REQUIRED","version":"FORBIDDEN","attempt":"REQUIRED","idempotency":"FORBIDDEN"})] | length) == 1 and
  all(.policy.operation_bindings[] | select(.target_workload_id != "secret-broker" and .target_workload_id != "stt-tts-service");
    .target_workload_id == "control-plane") and
  ([.policy.authority_proof_producers[] |
    select(.producer_id == "secret-broker.provider-credential-materializer")] | length) == 1 and
  all(.policy.authority_proof_producers[] |
    select(.producer_id == "secret-broker.provider-credential-materializer");
    .caller_workload_id == "control-plane" and
    .owner_workload_id == "control-plane" and
    .application_credential == "PLATFORM_WORKER_GRANT" and
    .application_credential_issuer == "https://control-plane.kodex-system.svc.cluster.local/authority/platform-worker/control-plane" and
    .application_credential_audience == "urn:kodex:platform-worker:control-plane" and
    .application_credential_trust_bundle_id == "control-plane-platform-worker-grants-g1" and
    .authority_sources == ["DOMAIN_STATE"] and
    (.allowed_operation_ids | sort) == provider_operations) and
  ([.policy.authority_proof_producers[] | select(.producer_id == "secret-broker.runtime-credential-projection" and
    .caller_workload_id == "runtime-controller" and .allowed_operation_ids ==
      ["platform.runtime.credentials.materialize", "platform.runtime.credentials.readiness.check", "platform.runtime.credentials.system-assistant.materialize"])] | length) == 1 and
	([.policy.authority_proof_producers[] | select(.caller_workload_id == "stt-tts-service")] | length) == 0
' "$canonical" >/dev/null || fail 'canonical policy invariants are invalid'

printf 'Authority policy codegen tests passed\n'
