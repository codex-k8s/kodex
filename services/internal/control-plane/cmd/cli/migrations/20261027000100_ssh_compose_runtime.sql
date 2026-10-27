-- +goose Up
SET ROLE control_plane_owner;
SET LOCAL lock_timeout='8s';
SET LOCAL statement_timeout='30s';
ALTER TABLE control_plane.integration_definitions
  DROP CONSTRAINT integration_definitions_adapter_check,
  ADD CONSTRAINT integration_definitions_adapter_check CHECK (adapter IN (
    'SYNTHETIC_HTTP','GITHUB','GITLAB','JIRA','CONFLUENCE','EMAIL_HTTPS',
    'MATTERMOST_INTERACTION','HTTPS_JSON_READ','OPENAPI_MCP','CONTEXT7','SSH_COMPOSE'
  ));
ALTER TABLE control_plane.integration_grants
  DROP CONSTRAINT integration_grants_resource_kind_check,
  ADD CONSTRAINT integration_grants_resource_kind_check CHECK (resource_kind IN (
    'SYNTHETIC_JOURNAL','GITHUB_REPOSITORY','GITLAB_PROJECT','JIRA_PROJECT',
    'CONFLUENCE_SPACE','EMAIL_SENDER','MATTERMOST_CHANNEL','HTTPS_RESOURCE','SSH_TARGET'
  ));
ALTER TABLE control_plane.integration_invocations
  DROP CONSTRAINT integration_invocations_resource_kind_check,
  ADD CONSTRAINT integration_invocations_resource_kind_check CHECK (resource_kind IN (
    'SYNTHETIC_JOURNAL','GITHUB_REPOSITORY','GITLAB_PROJECT','JIRA_PROJECT',
    'CONFLUENCE_SPACE','EMAIL_SENDER','MATTERMOST_CHANNEL','HTTPS_RESOURCE','SSH_TARGET'
  ));
ALTER TABLE control_plane.runtime_secrets ADD COLUMN purpose text NOT NULL DEFAULT 'APPLICATION'
  CHECK (purpose IN ('APPLICATION','SSH_COMPOSE'));
ALTER TABLE control_plane.integration_invocations
  ADD COLUMN ssh_compose_snapshot jsonb,
  ADD COLUMN ssh_compose_deadline timestamptz,
  ADD COLUMN ssh_compose_recovery boolean NOT NULL DEFAULT false,
  ADD COLUMN ssh_compose_admission_blocked boolean NOT NULL DEFAULT false,
  ADD COLUMN ssh_compose_admission_block_reason text NOT NULL DEFAULT '',
  ADD CONSTRAINT ssh_compose_admission_block_shape CHECK (
    (NOT ssh_compose_admission_blocked AND ssh_compose_admission_block_reason='') OR
    (ssh_compose_admission_blocked AND ssh_compose_admission_block_reason='CURRENT_ADMISSION_DENIED')
  ),
  ADD CONSTRAINT ssh_compose_snapshot_bounds CHECK (
    ssh_compose_snapshot IS NULL OR
    (jsonb_typeof(ssh_compose_snapshot)='object' AND octet_length(ssh_compose_snapshot::text)<=32768)
  );
ALTER TABLE control_plane.integration_connection_tests
  ADD COLUMN ssh_compose_snapshot jsonb,
  ADD CONSTRAINT ssh_compose_test_snapshot_bounds CHECK (
    ssh_compose_snapshot IS NULL OR
    (jsonb_typeof(ssh_compose_snapshot)='object' AND octet_length(ssh_compose_snapshot::text)<=32768)
  );

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_ssh_compose_snapshot()
RETURNS trigger LANGUAGE plpgsql SET search_path = pg_catalog AS $$
BEGIN
  IF OLD.ssh_compose_snapshot IS NOT NULL AND OLD.ssh_compose_snapshot IS DISTINCT FROM NEW.ssh_compose_snapshot THEN
    RAISE EXCEPTION 'SSH Compose snapshot is immutable';
  END IF;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER ssh_compose_invocation_snapshot_immutable
BEFORE UPDATE ON control_plane.integration_invocations
FOR EACH ROW EXECUTE FUNCTION control_plane.guard_ssh_compose_snapshot();
CREATE TRIGGER ssh_compose_test_snapshot_immutable
BEFORE UPDATE ON control_plane.integration_connection_tests
FOR EACH ROW EXECUTE FUNCTION control_plane.guard_ssh_compose_snapshot();

-- Блокировка допуска не превращает неизвестный эффект в terminal verdict.
-- Lease/pins остаются прежними; разблокирования в этом профиле нет.
-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_ssh_compose_admission_block()
RETURNS trigger LANGUAGE plpgsql SET search_path = pg_catalog AS $$
BEGIN
  IF OLD.ssh_compose_admission_blocked OR NEW.ssh_compose_admission_blocked THEN
    IF NEW.ssh_compose_snapshot IS NULL
       OR NEW.operation NOT IN ('ssh.compose.inspect','ssh.compose.deploy','ssh.compose.status')
       OR NEW.effect_receipt_id IS NOT NULL
       OR (OLD.ssh_compose_admission_blocked AND
          (NOT NEW.ssh_compose_admission_blocked OR
           NEW.ssh_compose_admission_block_reason IS DISTINCT FROM OLD.ssh_compose_admission_block_reason))
       OR ROW(NEW.id,NEW.organization_id,NEW.run_id,NEW.node_id,NEW.connection_id,
              NEW.grant_id,NEW.grant_version,NEW.capability_key,NEW.idempotency_key,NEW.intent_digest,NEW.approval_scope_id,NEW.approval_scope_paths,NEW.mailbox_gate_required,NEW.definition_version,NEW.definition_digest,
              NEW.operation,NEW.risk,NEW.approval_policy,NEW.resource_kind,NEW.resource_scope,
              NEW.resource_scope_digest,NEW.effect_key,NEW.input_digest,NEW.bounded_input,
              NEW.ssh_compose_snapshot,NEW.ssh_compose_deadline,NEW.ssh_compose_recovery,
              NEW.generation,NEW.claimed_workload,NEW.claimed_lease_ref,NEW.claimed_fence_digest,
              NEW.effect_receipt_id)
          IS DISTINCT FROM
          ROW(OLD.id,OLD.organization_id,OLD.run_id,OLD.node_id,OLD.connection_id,
              OLD.grant_id,OLD.grant_version,OLD.capability_key,OLD.idempotency_key,OLD.intent_digest,OLD.approval_scope_id,OLD.approval_scope_paths,OLD.mailbox_gate_required,OLD.definition_version,OLD.definition_digest,
              OLD.operation,OLD.risk,OLD.approval_policy,OLD.resource_kind,OLD.resource_scope,
              OLD.resource_scope_digest,OLD.effect_key,OLD.input_digest,OLD.bounded_input,
              OLD.ssh_compose_snapshot,OLD.ssh_compose_deadline,OLD.ssh_compose_recovery,
              OLD.generation,OLD.claimed_workload,OLD.claimed_lease_ref,OLD.claimed_fence_digest,
              OLD.effect_receipt_id)
       OR NEW.lease_ref IS NOT NULL OR NEW.effect_fence_digest IS NOT NULL
       OR NEW.workload_instance IS NOT NULL OR NEW.lease_expires_at IS NOT NULL
       OR OLD.state NOT IN ('READY','UNKNOWN_OUTCOME','FAILED','CANCELLED')
       OR (NOT OLD.ssh_compose_admission_blocked AND NEW.state IS DISTINCT FROM OLD.state)
       OR (OLD.state='UNKNOWN_OUTCOME' AND
           ROW(NEW.state,NEW.result_summary,NEW.safe_error_code) IS DISTINCT FROM
           ROW(OLD.state,OLD.result_summary,OLD.safe_error_code))
       OR (OLD.state<>'UNKNOWN_OUTCOME' AND NEW.state NOT IN ('READY','FAILED','CANCELLED')) THEN
      RAISE EXCEPTION 'SSH Compose blocked admission is immutable';
    END IF;
  END IF;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER ssh_compose_admission_block_immutable
BEFORE UPDATE ON control_plane.integration_invocations
FOR EACH ROW EXECUTE FUNCTION control_plane.guard_ssh_compose_admission_block();
REVOKE ALL ON FUNCTION control_plane.guard_ssh_compose_admission_block() FROM PUBLIC;

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_ssh_compose_secret_purpose()
RETURNS trigger LANGUAGE plpgsql SET search_path = pg_catalog AS $$
BEGIN
  IF OLD.purpose='SSH_COMPOSE' AND NEW.purpose<>'SSH_COMPOSE' THEN
    RAISE EXCEPTION 'SSH Compose credential purpose is irreversible';
  END IF;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER ssh_compose_secret_purpose_irreversible
BEFORE UPDATE ON control_plane.runtime_secrets
FOR EACH ROW EXECUTE FUNCTION control_plane.guard_ssh_compose_secret_purpose();
REVOKE ALL ON FUNCTION control_plane.guard_ssh_compose_snapshot() FROM PUBLIC;
REVOKE ALL ON FUNCTION control_plane.guard_ssh_compose_secret_purpose() FROM PUBLIC;



-- Только closed DEPLOYMENT добавляется к действующему logical effect guard010.
-- Прежние prefix/cap16/full execution tuple/права удаления сохраняются побайтно.
-- +goose StatementBegin
DO $migration$
DECLARE v_source text; v_updated text;
BEGIN
    SELECT prosrc INTO STRICT v_source FROM pg_proc WHERE oid='control_plane.guard_workflow_effect_records()'::regprocedure;
    IF md5(v_source) IS DISTINCT FROM '74f7d5fd475cb0919b76579335b547a5' THEN
        RAISE EXCEPTION 'workflow effect guard source changed';
    END IF;
    v_updated:=replace(v_source,$old$OR (requirement.value->>'Kind'='INTEGRATION_RECEIPT' AND requirement.value->>'Operation'='github.pull_request.merge' AND invocation.operation='github.pull_request.merge'))$old$,$new$OR (requirement.value->>'Kind'='INTEGRATION_RECEIPT' AND requirement.value->>'Operation'='github.pull_request.merge' AND invocation.operation='github.pull_request.merge')
                    OR (requirement.value->>'Kind'='DEPLOYMENT' AND requirement.value->>'Operation'='ssh.compose.deploy'
                        AND invocation.operation='ssh.compose.deploy' AND invocation.resource_kind='SSH_TARGET' AND connection.definition_key='ssh-compose'
                        AND invocation.resource_scope->>'target_ref'=requirement.value->>'TargetRef'
                        AND invocation.ssh_compose_snapshot->'target'->>'ref'=requirement.value->>'TargetRef'
                        AND invocation.ssh_compose_snapshot->>'originRuntimeRevisionId'=revision.id::text
                        AND invocation.ssh_compose_snapshot->>'originLeaseId'=lease.id::text
                        AND invocation.ssh_compose_snapshot->'originGeneration'=to_jsonb(revision.generation)
                        AND invocation.ssh_compose_snapshot->>'deploymentRef'=invocation.ref
                        AND invocation.ssh_compose_snapshot->>'deploymentInputDigest'=invocation.input_digest
                        AND invocation.ssh_compose_snapshot->>'deploymentEffectKey'=invocation.effect_key
                        AND EXISTS (
                            SELECT 1 FROM jsonb_array_elements(origin.workflow_inputs) dependency
                            JOIN control_plane.run_nodes source ON source.id::text=dependency->>'sourceNodeId'
                                AND source.organization_id=origin.organization_id AND source.root_run_id=origin.root_run_id
                                AND source.workflow_step_origin_node_id IS NULL AND source.workflow_business_attempt=1
                                AND source.workflow_step_key=dependency->>'stepKey'
                            CROSS JOIN LATERAL jsonb_array_elements(source.workflow_attempts) accepted
                            CROSS JOIN LATERAL jsonb_array_elements(accepted->'results') result
                            JOIN control_plane.integration_effect_receipts merge_receipt ON merge_receipt.ref=result->>'receiptRef'
                                AND merge_receipt.organization_id=origin.organization_id
                            JOIN control_plane.integration_invocations merge_invocation ON merge_invocation.id=merge_receipt.invocation_id
                                AND merge_invocation.organization_id=origin.organization_id AND merge_invocation.state='SUCCEEDED'
                                AND merge_invocation.operation='github.pull_request.merge'
                                AND merge_invocation.effect_receipt_id=merge_receipt.id AND merge_invocation.effect_key=merge_receipt.effect_key
                                AND merge_invocation.input_digest=merge_receipt.input_digest
                            WHERE dependency->>'stepKey'=requirement.value->>'SubjectStepKey'
                                AND step.value->'DependsOn' ? (dependency->>'stepKey')
                                AND accepted=dependency->'receipt' AND accepted->>'accepted'='true'
                                AND result->>'kind'='INTEGRATION_RECEIPT' AND result->>'repository'=requirement.value->>'Repository'
                                AND result->>'headSha'=invocation.ssh_compose_snapshot->>'mergedSha'
                                AND merge_invocation.ref=invocation.ssh_compose_snapshot->>'mergeInvocationRef'
                        )))$new$);
    IF v_updated=v_source THEN RAISE EXCEPTION 'SSH workflow effect preimage missing'; END IF;
    EXECUTE format('CREATE OR REPLACE FUNCTION control_plane.guard_workflow_effect_records() RETURNS trigger LANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog,control_plane AS %L',v_updated);
END;
$migration$;
-- +goose StatementEnd
GRANT EXECUTE ON FUNCTION control_plane.guard_ssh_compose_snapshot(),control_plane.guard_ssh_compose_secret_purpose() TO control_plane_runtime;
GRANT EXECUTE ON FUNCTION control_plane.guard_ssh_compose_admission_block() TO control_plane_runtime;
REVOKE ALL ON FUNCTION control_plane.guard_workflow_effect_records() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.guard_workflow_effect_records() TO control_plane_runtime;
RESET ROLE;

-- +goose Down
SELECT 1;
