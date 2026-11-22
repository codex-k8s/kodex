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
candidate="$temporary_directory/authority-policy-111.json"

(
  cd -- "$repository_root/libs/go/controlplaneclient"
  env -u GOFLAGS GOENV=off GOWORK=off go run ./cmd/policygen \
    --output "$generated" \
    --oidc-issuer '__KODEX_OIDC_ISSUER__' \
    --oidc-audience kodex-control-api
)

cmp -s "$generated" "$canonical" || fail 'generated policy differs from the canonical file'
(
  cd -- "$repository_root/libs/go/controlplaneclient"
  env -u GOFLAGS GOENV=off GOWORK=off go run ./cmd/policygen \
    --output "$candidate" --policy-revision 111 \
    --oidc-issuer '__KODEX_OIDC_ISSUER__' --oidc-audience kodex-control-api
)
jq -e --slurpfile predecessor "$generated" '
  "platform.integrations.kubernetes.file.cleanup.repair" as $operation |
  .policy_revision == 111 and
  ([.policy.operation_bindings[] | select(.operation_id == $operation)] |
    length == 1 and all(.[];
      .full_method == "/controlplane.v1.ManagedKubernetesFileService/RepairManagedKubernetesFileCleanup" and
      .caller_workload_id == "control-api-gateway" and .target_workload_id == "control-plane" and
      .authority_proof_producer_id == "control-plane.oidc" and .permission == $operation and
      .authority_sources == ["OIDC_SESSION", "DOMAIN_STATE"] and .project_required == false and
      .request_profile == {mode:"UNARY_PROTO_SHA256",resource:"FORBIDDEN",version:"FORBIDDEN",attempt:"FORBIDDEN",idempotency:"FORBIDDEN"})) and
  ([.policy.authority_proof_producers[] | select(.allowed_operation_ids | index($operation)) |
    .producer_id] == ["control-plane.oidc"]) and
  ((.policy_revision = 110 |
     .policy.operation_bindings |= map(select(.operation_id != $operation)) |
     .policy.authority_proof_producers[] |=
       if .producer_id == "control-plane.oidc"
       then .allowed_operation_ids |= map(select(. != $operation))
       else .
       end) == $predecessor[0])
' "$candidate" >/dev/null || fail 'candidate policy must add exactly the owner FILE cleanup operation'
jq -e '
  def provider_operations: [
    "platform.provider-accounts.credential.renew",
    "platform.provider-accounts.credential.renewal.observe",
    "platform.provider-accounts.model-catalog.observe",
    "platform.provider-credentials.api-key.materialize",
    "platform.provider-credentials.cleanup",
    "platform.provider-credentials.device-authorize.get",
    "platform.provider-credentials.device-authorize.start",
    "platform.provider-credentials.materialization.discard",
    "platform.provider-credentials.readiness.check"
  ];
  .v == 1 and .policy.default_decision == "DENY" and
  .policy_revision == 110 and .policy.authority_abi_version == 2 and
  ([
    .policy.operation_bindings[] |
    select(.full_method | startswith("/controlplane.v1.PersonalEmailMailboxService/"))
   ] as $email |
   ([$email[].operation_id] | sort) == [
    "platform.email-mailboxes.mine.create", "platform.email-mailboxes.mine.list",
    "platform.email-mailboxes.ownership.configure", "platform.email-mailboxes.ownership.read",
    "platform.email-mailboxes.ownership.revoke", "platform.email-mailboxes.receipts.read"
   ] and all($email[];
     .caller_workload_id == "control-api-gateway" and .target_workload_id == "control-plane" and
     .authority_proof_producer_id == "control-plane.oidc" and .authority_sources == ["OIDC_SESSION", "DOMAIN_STATE"] and
     .project_required == false and .permission == .operation_id and
     .request_profile.mode == "UNARY_PROTO_SHA256" and .request_profile.attempt == "FORBIDDEN")) and
  ([.policy.operation_bindings[] | select(.full_method | test("\\.ManagedSSH"))] as $ssh |
   ($ssh | length) == 15 and
   ([$ssh[] | select(.operation_id | startswith("platform.integrations.ssh.setup.")) |
     select(.caller_workload_id == "control-api-gateway" and .target_workload_id == "control-plane" and
       .authority_sources == ["OIDC_SESSION", "DOMAIN_STATE"])] | length) == 3 and
   ([$ssh[] | select(.operation_id | test("^platform\\.runtime\\.ssh\\.(setup|health|execution)\\.")) |
     select(.caller_workload_id == "secret-broker" and .target_workload_id == "control-plane" and
       .authority_sources == ["DOMAIN_STATE"])] | length) == 8 and
   ([$ssh[] | select(.operation_id == "platform.runtime.ssh.invocation.read") |
     select(.caller_workload_id == "runtime-controller" and .target_workload_id == "control-plane" and
       .authority_sources == ["DOMAIN_STATE"])] | length) == 1 and
   ([$ssh[] | select(.operation_id | IN("ssh.health.probe", "ssh.execute", "ssh.reconcile")) |
     select(.caller_workload_id == "integration-gateway" and .target_workload_id == "secret-broker" and
       .authority_proof_producer_id == "secret-broker.managed-ssh" and
       .authority_sources == ["DOMAIN_STATE"])] | length) == 3 and
   all($ssh[]; .permission == .operation_id and .continuation == null and
     .project_required == false and .request_profile ==
       {"mode":"UNARY_PROTO_SHA256","resource":"FORBIDDEN","version":"FORBIDDEN","attempt":"FORBIDDEN","idempotency":"FORBIDDEN"})) and
  ([.policy.operation_bindings[] | select(.operation_id == "platform.command.telegram-subscription.configure") |
    select(.caller_workload_id == "control-api-gateway" and .target_workload_id == "control-plane" and
      .authority_proof_producer_id == "control-plane.oidc" and .project_required == true and
      .permission == .operation_id and .authority_sources == ["OIDC_SESSION", "DOMAIN_STATE"] and
      .request_profile == {"mode":"UNARY_PROTO_SHA256","resource":"FORBIDDEN","version":"FORBIDDEN","attempt":"FORBIDDEN","idempotency":"REQUIRED"})] | length) == 1 and
  ([.policy.operation_bindings[] | select(.operation_id == "platform.query.telegram-subscription.get" or .operation_id == "platform.query.interaction-deliveries.list") |
    select(.caller_workload_id == "control-api-gateway" and .target_workload_id == "control-plane" and
      .authority_proof_producer_id == "control-plane.oidc" and .project_required == true and
      .permission == .operation_id and .authority_sources == ["OIDC_SESSION", "DOMAIN_STATE"] and
      .request_profile == {"mode":"UNARY_PROTO_SHA256","resource":"FORBIDDEN","version":"FORBIDDEN","attempt":"FORBIDDEN","idempotency":"FORBIDDEN"})] | length) == 2 and
  ([.policy.operation_bindings[] | select(.operation_id == "platform.interactions.deliveries.begin") |
    select(.caller_workload_id == "interaction-gateway" and .target_workload_id == "control-plane" and
      .authority_proof_producer_id == "control-plane.interaction-gateway" and .project_required == false and
      .permission == .operation_id and .authority_sources == ["DOMAIN_STATE"] and
      .request_profile == {"mode":"UNARY_PROTO_SHA256","resource":"FORBIDDEN","version":"FORBIDDEN","attempt":"FORBIDDEN","idempotency":"FORBIDDEN"})] | length) == 1 and
	([.policy.operation_bindings[] | select(.operation_id == "platform.oidc.session.resolve" or
	  .operation_id == "platform.oidc.session.closure.get" or .operation_id == "platform.oidc.session.revoke") |
	  select(.caller_workload_id == "control-api-gateway" and .target_workload_id == "control-plane" and
	    .authority_proof_producer_id == "control-plane.oidc" and .project_required == false and .permission == .operation_id and
	    .request_profile == {"mode":"UNARY_PROTO_SHA256","resource":"FORBIDDEN","version":"FORBIDDEN","attempt":"FORBIDDEN","idempotency":"FORBIDDEN"})] | length) == 3 and
  ([.policy.operation_bindings[] | select(.operation_id == "platform.provider-accounts.credential.renewal.work.check" or
      .operation_id == "platform.provider-accounts.credential.renewal.authorize" or .operation_id == "platform.provider-accounts.credential.renewal.validate") |
    select(.caller_workload_id == "secret-broker" and .target_workload_id == "control-plane" and
      .authority_proof_producer_id == "control-plane.secret-broker" and .project_required == false and
      .permission == .operation_id and .authority_sources == ["DOMAIN_STATE"] and
      .request_profile == {"mode":"UNARY_PROTO_SHA256","resource":"FORBIDDEN","version":"FORBIDDEN","attempt":"FORBIDDEN","idempotency":"FORBIDDEN"})] | length) == 3 and
  ([.policy.operation_bindings[] | select(.operation_id == "platform.query.artifact-revisions.list" or .operation_id == "platform.query.artifact-revisions.get") |
    select(.caller_workload_id == "control-api-gateway" and .target_workload_id == "control-plane" and .project_required == false and
      .authority_proof_producer_id == "control-plane.oidc" and .authority_sources == ["OIDC_SESSION", "DOMAIN_STATE"] and
      .request_profile == {"mode":"UNARY_PROTO_SHA256","resource":"REQUIRED","version":"FORBIDDEN","attempt":"FORBIDDEN","idempotency":"FORBIDDEN"})] | length) == 2 and
  ([.policy.operation_bindings[] | select(.operation_id == "platform.command.artifact-revisions.download") |
    select(.caller_workload_id == "control-api-gateway" and .target_workload_id == "control-plane" and .project_required == false and
      .authority_proof_producer_id == "control-plane.oidc" and .authority_sources == ["OIDC_SESSION", "DOMAIN_STATE"] and
      .request_profile == {"mode":"STREAM_SESSION","resource":"FORBIDDEN","version":"REQUIRED","attempt":"FORBIDDEN","idempotency":"REQUIRED"})] | length) == 1 and
  ([.policy.operation_bindings[] | select(.operation_id == "platform.query.organization.role-images.vulnerability-report.get" or .operation_id == "platform.query.role-images.vulnerability-report.get") |
    select(.caller_workload_id == "control-api-gateway" and .target_workload_id == "control-plane" and .project_required == false and
      .authority_proof_producer_id == "control-plane.oidc" and .authority_sources == ["OIDC_SESSION", "DOMAIN_STATE"] and
      .request_profile == {"mode":"UNARY_PROTO_SHA256","resource":"REQUIRED","version":"FORBIDDEN","attempt":"FORBIDDEN","idempotency":"FORBIDDEN"})] | length) == 2 and
  ([.policy.operation_bindings[] | select(.operation_id == "platform.command.organization.role-images.risk.decide" or .operation_id == "platform.command.role-images.risk.decide") |
    select(.caller_workload_id == "control-api-gateway" and .target_workload_id == "control-plane" and .project_required == false and
      .authority_proof_producer_id == "control-plane.oidc" and .authority_sources == ["OIDC_SESSION", "DOMAIN_STATE"] and
      .request_profile == {"mode":"UNARY_PROTO_SHA256","resource":"REQUIRED","version":"REQUIRED","attempt":"FORBIDDEN","idempotency":"REQUIRED"})] | length) == 2 and
  all(.policy.operation_bindings[] | select(.operation_id | contains("role-images.risk.decide") or contains("vulnerability-report.get"));
    .caller_workload_id == "control-api-gateway" and .permission == .operation_id) and
  ([.policy.operation_bindings[] | select(.operation_id == "platform.role-images.admission.fail" or .operation_id == "platform.role-images.admission.expire") |
    select(.caller_workload_id == "image-admission" and .target_workload_id == "control-plane" and .project_required == false and
      .authority_proof_producer_id == "control-plane.image-admission" and
      .request_profile == {"mode":"UNARY_PROTO_SHA256","resource":"FORBIDDEN","version":"FORBIDDEN","attempt":"FORBIDDEN","idempotency":"FORBIDDEN"})] | length) == 2 and
  ([.policy.operation_bindings[] | select(.operation_id == "platform.command.system-assistant.integration-grants.change") |
    select(.caller_workload_id == "control-api-gateway" and .target_workload_id == "control-plane" and .project_required == false and
      .authority_proof_producer_id == "control-plane.oidc" and
      .request_profile == {"mode":"UNARY_PROTO_SHA256","resource":"FORBIDDEN","version":"REQUIRED","attempt":"FORBIDDEN","idempotency":"REQUIRED"})] | length) == 1 and
  ([.policy.operation_bindings[] | select(.operation_id == "platform.query.system-assistant.integration-grant-candidates.get") |
    select(.caller_workload_id == "control-api-gateway" and .target_workload_id == "control-plane" and .project_required == false and
      .authority_proof_producer_id == "control-plane.oidc" and
      .request_profile == {"mode":"UNARY_PROTO_SHA256","resource":"FORBIDDEN","version":"FORBIDDEN","attempt":"FORBIDDEN","idempotency":"FORBIDDEN"})] | length) == 1 and
  ([.policy.operation_bindings[] | select(.operation_id | contains("organization.runtime") or contains("organization.role-images")) |
    select(.caller_workload_id == "control-api-gateway" and .target_workload_id == "control-plane" and
      .authority_proof_producer_id == "control-plane.oidc" and .project_required == false and
      .request_profile.mode == "UNARY_PROTO_SHA256" and .request_profile.attempt == "FORBIDDEN")] | length) == 10 and
  ([.policy.operation_bindings[] | select(.operation_id == "platform.command.organization.runtime-secret-drafts.create" or
      .operation_id == "platform.command.organization.runtime-environment-drafts.create" or
      .operation_id == "platform.organization.role-images.recipes.manage") |
    select(.request_profile == {"mode":"UNARY_PROTO_SHA256","resource":"FORBIDDEN","version":"FORBIDDEN","attempt":"FORBIDDEN","idempotency":"REQUIRED"})] | length) == 3 and
  ([.policy.operation_bindings[] | select(.operation_id == "platform.command.organization.role-images.promote") |
    select(.request_profile == {"mode":"UNARY_PROTO_SHA256","resource":"REQUIRED","version":"REQUIRED","attempt":"FORBIDDEN","idempotency":"REQUIRED"})] | length) == 1 and
  ([.policy.operation_bindings[] | select(.operation_id == "platform.organization.role-images.recipes.get" or
      .operation_id == "platform.organization.role-images.recipe-revisions.list") |
    select(.request_profile == {"mode":"UNARY_PROTO_SHA256","resource":"REQUIRED","version":"FORBIDDEN","attempt":"FORBIDDEN","idempotency":"FORBIDDEN"})] | length) == 2 and
  ([.policy.operation_bindings[] | select(.operation_id == "platform.organization.role-images.recipes.list" or
      .operation_id == "platform.query.organization.runtime-secrets.list") |
    select(.request_profile == {"mode":"UNARY_PROTO_SHA256","resource":"FORBIDDEN","version":"FORBIDDEN","attempt":"FORBIDDEN","idempotency":"FORBIDDEN"})] | length) == 2 and
  ([.policy.operation_bindings[] | select(.operation_id == "platform.command.role-images.copy" or .operation_id == "platform.command.integration-definitions.copy" or .operation_id == "platform.command.role-images.archive-configuration" or .operation_id == "platform.command.integration-definitions.archive") |
    select(.caller_workload_id == "control-api-gateway" and .target_workload_id == "control-plane" and .project_required == false and
      .request_profile == {"mode":"UNARY_PROTO_SHA256","resource":"REQUIRED","version":"REQUIRED","attempt":"FORBIDDEN","idempotency":"REQUIRED"})] | length) == 4 and
  ([.policy.operation_bindings[] | select(.operation_id == "platform.command.prompt-templates.create-draft" or .operation_id == "platform.command.role-image-revisions.create-draft") |
    select(.caller_workload_id == "control-api-gateway" and .target_workload_id == "control-plane" and .project_required == true)] | length) == 2 and
  ([.policy.operation_bindings[] | select(.operation_id == "platform.command.integration-definitions.create-draft") |
    select(.caller_workload_id == "control-api-gateway" and .target_workload_id == "control-plane" and .project_required == false)] | length) == 1 and
  ([.policy.operation_bindings[] | select(.operation_id == "platform.query.provider-accounts.blockers.list") |
    select(.caller_workload_id == "control-api-gateway" and .target_workload_id == "control-plane" and
      .request_profile == {"mode":"UNARY_PROTO_SHA256","resource":"REQUIRED","version":"FORBIDDEN","attempt":"FORBIDDEN","idempotency":"FORBIDDEN"})] | length) == 1 and
  ([.policy.operation_bindings[] | select(.operation_id == "platform.command.provider-accounts.queued-work.cancel" or .operation_id == "platform.command.provider-accounts.concurrency.set") |
    select(.caller_workload_id == "control-api-gateway" and .target_workload_id == "control-plane" and
      .request_profile == {"mode":"UNARY_PROTO_SHA256","resource":"REQUIRED","version":"REQUIRED","attempt":"FORBIDDEN","idempotency":"REQUIRED"})] | length) == 2 and
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
	(.policy.authority_proof_producers | length) == 19 and
  ([.policy.operation_bindings[] | select(.caller_workload_id == "integration-gateway" and .target_workload_id == "secret-broker") | .operation_id]) ==
    ["kubernetes.cancel", "kubernetes.execute", "kubernetes.readiness", "kubernetes.stream.exchange", "platform.ssh-compose.credential.project", "ssh.execute", "ssh.health.probe", "ssh.reconcile"] and
  ([.policy.operation_bindings[] | select(.operation_id == "platform.ssh-compose.credential.project" and
    .full_method == "/secretbroker.v1.RuntimeCredentialProjectionService/ProjectSSHComposeCredential" and
    .authority_proof_producer_id == "secret-broker.ssh-compose-projection" and
    .permission == "platform.ssh-compose.credential.project" and .project_required == false and
    .authority_sources == ["DOMAIN_STATE"])] | length) == 1 and
  ([.policy.authority_proof_producers[] | select(.producer_id == "secret-broker.ssh-compose-projection" and
    .caller_workload_id == "integration-gateway" and .owner_workload_id == "control-plane" and
    .application_credential == "PLATFORM_WORKER_GRANT" and
    .allowed_operation_ids == ["platform.ssh-compose.credential.project"])] | length) == 1 and
  ([.policy.authority_proof_producers[] | select(.caller_workload_id == "image-admission-controller")] | length) == 0 and
  ([.policy.operation_bindings[] | select(.caller_workload_id == "image-admission-controller")] | length) == 0 and
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
    .project_required == (.operation_id == "platform.runtime.credentials.materialize") and .authority_sources == ["DOMAIN_STATE", "OIDC_SESSION", "RUNTIME_EXECUTION"]) and
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
      .full_method == "/stt.v1.SpeechToTextService/Transcribe" and .project_required == false and .permission == "platform.stt.transcribe" and
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

jq -e '
  def nested_unary: {"mode":"UNARY_PROTO_SHA256","resource":"FORBIDDEN","version":"FORBIDDEN","attempt":"FORBIDDEN","idempotency":"FORBIDDEN"};
  ([.policy.operation_bindings[] | select(.full_method | contains(".ManagedKubernetes"))] | length) == 56 and
  ([.policy.operation_bindings[] |
    select(.operation_id | IN("platform.integrations.kubernetes.file.list", "platform.integrations.kubernetes.file.get")) |
    select(.caller_workload_id == "control-api-gateway" and .target_workload_id == "control-plane" and
      .authority_proof_producer_id == "control-plane.oidc" and .authority_sources == ["OIDC_SESSION", "DOMAIN_STATE"] and
      (.full_method | startswith("/controlplane.v1.ManagedKubernetesFileService/")))] | length) == 2 and
  ([.policy.operation_bindings[] | select(.operation_id == "platform.runtime.kubernetes.file.prepare") |
    select(.caller_workload_id == "secret-broker" and .target_workload_id == "control-plane" and
      .authority_proof_producer_id == "control-plane.secret-broker" and .authority_sources == ["DOMAIN_STATE"] and
      .full_method == "/controlplane.v1.ManagedKubernetesOwnerService/PrepareManagedKubernetesFileInput")] | length) == 1 and
  all(.policy.operation_bindings[] | select(.full_method | contains(".ManagedKubernetes"));
    .request_profile == nested_unary and .project_required == false and .permission == .operation_id) and
  ([.policy.operation_bindings[] | select(.operation_id == "platform.integrations.kubernetes.capture.get" or .operation_id == "platform.integrations.kubernetes.capture.repair" or .operation_id == "platform.integrations.kubernetes.capture.list" or .operation_id == "platform.integrations.kubernetes.capture.cleanup.repair") | select(.caller_workload_id == "control-api-gateway" and .authority_proof_producer_id == "control-plane.oidc" and .authority_sources == ["OIDC_SESSION", "DOMAIN_STATE"] and (.full_method | startswith("/controlplane.v1.ManagedKubernetesCaptureService/")))] | length) == 4 and
  ([.policy.operation_bindings[] | select(.operation_id | startswith("platform.integrations.kubernetes.port-forward.")) | .operation_id] | sort) ==
    ["platform.integrations.kubernetes.port-forward.close", "platform.integrations.kubernetes.port-forward.get",
     "platform.integrations.kubernetes.port-forward.list", "platform.integrations.kubernetes.port-forward.open"] and
  all(.policy.operation_bindings[] | select(.operation_id | startswith("platform.integrations.kubernetes.port-forward."));
    .caller_workload_id == "control-api-gateway" and .authority_proof_producer_id == "control-plane.oidc" and
    .target_workload_id == "control-plane" and .authority_sources == ["OIDC_SESSION", "DOMAIN_STATE"] and
    (.full_method | startswith("/controlplane.v1.ManagedKubernetesPortForwardConnectionService/"))) and
  ([.policy.operation_bindings[] | select(.operation_id | startswith("platform.artifacts.kubernetes-capture.")) | .operation_id] | sort) ==
    ["platform.artifacts.kubernetes-capture.finalize", "platform.artifacts.kubernetes-capture.get",
     "platform.artifacts.kubernetes-capture.readiness", "platform.artifacts.kubernetes-capture.renew"] and
  all(.policy.operation_bindings[] | select(.operation_id | startswith("platform.artifacts.kubernetes-capture."));
    .caller_workload_id == "artifact-retention" and .authority_proof_producer_id == "control-plane.artifact-retention" and
    .target_workload_id == "control-plane" and .authority_sources == ["DOMAIN_STATE"] and
    .target_spiffe_id == "spiffe://kodex.local/ns/kodex-system/sa/control-plane" and
    .target_tls_server_name == "control-plane.kodex-system.svc.cluster.local" and
    .target_trust_bundle_id == "kodex-internal-ca-g1") and
  ([.policy.authority_proof_producers[] | select(.producer_id == "control-plane.artifact-retention" and
    .caller_workload_id == "artifact-retention" and .owner_workload_id == "control-plane" and
    .application_credential == "PLATFORM_WORKER_GRANT" and
    .application_credential_issuer == "https://control-plane.kodex-system.svc.cluster.local/authority/platform-worker/artifact-retention" and
    .application_credential_audience == "urn:kodex:platform-worker:artifact-retention" and
    .application_credential_trust_bundle_id == "artifact-retention-platform-worker-grants-g1" and
    .allowed_operation_ids == ["platform.artifacts.kubernetes-capture.finalize", "platform.artifacts.kubernetes-capture.get",
      "platform.artifacts.kubernetes-capture.readiness", "platform.artifacts.kubernetes-capture.renew"])] | length) == 1 and
  ([.policy.operation_bindings[] | select(.authority_proof_producer_id == "secret-broker.managed-kubernetes") |
    select(.caller_workload_id == "integration-gateway" and
      .caller_spiffe_id == "spiffe://kodex.local/ns/kodex-system/sa/integration-gateway" and
      .target_workload_id == "secret-broker" and
      .target_spiffe_id == "spiffe://kodex.local/ns/kodex-system/sa/secret-broker" and
      .target_tls_server_name == "secret-broker.kodex-system.svc.cluster.local" and
      .target_trust_bundle_id == "kodex-internal-ca-g1" and .authority_sources == ["DOMAIN_STATE"]) | .operation_id]) ==
    ["kubernetes.cancel", "kubernetes.execute", "kubernetes.readiness", "kubernetes.stream.exchange"] and
  ([.policy.authority_proof_producers[] | select(.producer_id == "secret-broker.managed-kubernetes" and
    .caller_workload_id == "integration-gateway" and .owner_workload_id == "control-plane" and
    .application_credential == "PLATFORM_WORKER_GRANT" and
    .application_credential_trust_bundle_id == "integration-gateway-platform-worker-grants-g1" and
    .allowed_operation_ids == ["kubernetes.cancel", "kubernetes.execute", "kubernetes.readiness", "kubernetes.stream.exchange"])] | length) == 1 and
  ([.policy.operation_bindings[] | select(.operation_id == "platform.runtime.kubernetes.invocation.read" and
    .caller_workload_id == "runtime-controller" and .authority_proof_producer_id == "control-plane.runtime-controller" and
    .full_method == "/controlplane.v1.ManagedKubernetesOwnerService/ReadManagedKubernetesInvocation")] | length) == 1
' "$canonical" >/dev/null || fail 'Kubernetes authority boundaries are invalid'

node "$repository_root/tools/release/service-identity-policy.mjs" check \
  "$canonical" \
  "$repository_root/services/internal/control-plane/internal/app/service-identity-classification.json" \
  "$repository_root/services/internal/control-plane/internal/app/service-identity-policy.json" >/dev/null ||
  fail 'target-owned service identity policy is incomplete'

printf 'Authority policy codegen tests passed\n'
