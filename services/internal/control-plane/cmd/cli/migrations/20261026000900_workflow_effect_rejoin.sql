-- +goose Up
SET ROLE control_plane_owner;
SET LOCAL lock_timeout='8s';
SET LOCAL statement_timeout='30s';

ALTER TABLE control_plane.run_nodes ADD CONSTRAINT workflow_effect_record_bounds
    CHECK (jsonb_array_length(workflow_effect_intents)<=16 AND jsonb_array_length(workflow_effect_rejoins)<=16);

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_workflow_effect_records() RETURNS trigger
LANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $$
DECLARE v_old_intents jsonb := '[]'; v_old_rejoins jsonb := '[]'; v_record jsonb; v_anchor uuid; v_count integer;
BEGIN
    IF TG_OP='DELETE' THEN
        IF OLD.workflow_effect_intents<>'[]'::jsonb OR OLD.workflow_effect_rejoins<>'[]'::jsonb THEN
            IF NOT control_plane.project_purge_authorized() OR NOT EXISTS (
                SELECT 1 FROM control_plane.project_purge_targets target
                JOIN control_plane.project_purge_context context USING (transaction_id)
                WHERE target.transaction_id=pg_current_xact_id_if_assigned() AND context.backend_pid=pg_backend_pid()
                    AND target.relation_id='control_plane.run_nodes'::regclass AND target.row_key=jsonb_build_array(OLD.id)
            ) THEN
                RAISE EXCEPTION 'workflow effect records require exact authorized purge' USING ERRCODE='23514',CONSTRAINT='workflow_effect_delete_authority';
            END IF;
        END IF;
        RETURN OLD;
    END IF;
    IF TG_OP='UPDATE' THEN v_old_intents:=OLD.workflow_effect_intents; v_old_rejoins:=OLD.workflow_effect_rejoins; END IF;
    IF NEW.workflow_effect_intents=v_old_intents AND NEW.workflow_effect_rejoins=v_old_rejoins THEN RETURN NEW; END IF;
    IF jsonb_typeof(NEW.workflow_effect_intents) IS DISTINCT FROM 'array' OR jsonb_typeof(NEW.workflow_effect_rejoins) IS DISTINCT FROM 'array'
       OR jsonb_array_length(NEW.workflow_effect_intents)<jsonb_array_length(v_old_intents)
       OR jsonb_array_length(NEW.workflow_effect_rejoins)<jsonb_array_length(v_old_rejoins)
       OR jsonb_array_length(NEW.workflow_effect_intents)+jsonb_array_length(NEW.workflow_effect_rejoins)
          <>jsonb_array_length(v_old_intents)+jsonb_array_length(v_old_rejoins)+1
       OR EXISTS (SELECT 1 FROM jsonb_array_elements(v_old_intents) WITH ORDINALITY item(value,n) WHERE NEW.workflow_effect_intents->(item.n::integer-1) IS DISTINCT FROM item.value)
       OR EXISTS (SELECT 1 FROM jsonb_array_elements(v_old_rejoins) WITH ORDINALITY item(value,n) WHERE NEW.workflow_effect_rejoins->(item.n::integer-1) IS DISTINCT FROM item.value) THEN
        RAISE EXCEPTION 'workflow effect records require one immutable append' USING ERRCODE='23514',CONSTRAINT='workflow_effect_append_only';
    END IF;
    PERFORM 1 FROM control_plane.runs root
    JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
    JOIN control_plane.workflows workflow ON workflow.id=version.workflow_id AND workflow.organization_id=root.organization_id AND workflow.project_id=root.project_id
    JOIN control_plane.subjects actor ON actor.id=root.initiated_by AND actor.organization_id=root.organization_id AND actor.active
    WHERE root.id=NEW.root_run_id AND root.organization_id=NEW.organization_id AND root.root_run_id=root.id
        AND root.state IN ('RUNNING','WAITING_HUMAN') AND version.spec->>'ExecutionMode'='SERVER_DAG_V1'
    FOR UPDATE OF root;
    IF NOT FOUND THEN RAISE EXCEPTION 'workflow effect root is not active' USING ERRCODE='23514',CONSTRAINT='workflow_effect_root'; END IF;
    v_anchor:=COALESCE(NEW.workflow_step_origin_node_id,NEW.id);
    IF jsonb_array_length(NEW.workflow_effect_intents)>jsonb_array_length(v_old_intents) THEN
        v_record:=NEW.workflow_effect_intents->(jsonb_array_length(v_old_intents));
        IF NEW.id<>v_anchor OR NEW.workflow_business_attempt<>1 OR (SELECT count(*) FROM jsonb_object_keys(v_record))<>21
           OR v_record->>'schema' IS DISTINCT FROM 'kodex.workflow-effect-intent.v1'
           OR COALESCE(v_record->>'semanticDigest','')!~'^[a-f0-9]{64}$' OR NOT EXISTS (
            SELECT 1 FROM control_plane.run_nodes origin
            JOIN control_plane.runtime_revisions revision ON revision.id::text=v_record->>'originRevisionId'
                AND revision.node_id=origin.id AND revision.run_id=origin.run_id AND revision.root_run_id=origin.root_run_id
                AND revision.organization_id=origin.organization_id AND revision.agent_id=origin.agent_id
                AND revision.turn_id=origin.turn_id AND revision.attempt=origin.attempt
            JOIN control_plane.runtime_leases lease ON lease.id::text=v_record->>'originLeaseId' AND lease.runtime_revision_id=revision.id
                AND lease.organization_id=revision.organization_id AND lease.node_id=origin.id AND lease.run_id=origin.run_id
                AND lease.generation=revision.generation AND lease.state='CLAIMED' AND lease.expires_at>clock_timestamp()
            JOIN control_plane.integration_invocations invocation ON invocation.node_id=origin.id AND invocation.run_id=origin.run_id
                AND invocation.organization_id=origin.organization_id AND invocation.ref=v_record->>'invocationRef' AND invocation.risk='WRITE'
            JOIN control_plane.integration_connections connection ON connection.id=invocation.connection_id AND connection.organization_id=invocation.organization_id
                AND connection.enabled AND connection.state='CONNECTED' AND connection.ref=v_record->>'connectionRef' AND to_jsonb(connection.version)=v_record->'connectionVersion'
            JOIN control_plane.integration_grants grant_binding ON grant_binding.id=invocation.grant_id AND grant_binding.organization_id=invocation.organization_id
                AND grant_binding.enabled AND grant_binding.ref=v_record->>'grantRef' AND grant_binding.version=invocation.grant_version
            JOIN control_plane.runs root ON root.id=origin.root_run_id AND root.organization_id=origin.organization_id
            JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
            CROSS JOIN LATERAL jsonb_array_elements(version.spec->'Steps') step(value)
            CROSS JOIN LATERAL jsonb_array_elements(step.value->'ResultRequirements') requirement(value)
            WHERE origin.id::text=v_record->>'originNodeId' AND origin.run_id::text=v_record->>'originRunId'
                AND origin.organization_id=NEW.organization_id AND origin.root_run_id=NEW.root_run_id AND origin.agent_id=NEW.agent_id
                AND origin.workflow_step_key=NEW.workflow_step_key AND COALESCE(origin.workflow_step_origin_node_id,origin.id)=NEW.id
                AND origin.state='RUNNING' AND to_jsonb(origin.attempt)=v_record->'originAttempt'
                AND to_jsonb(origin.workflow_business_attempt)=v_record->'originBusinessAttempt' AND to_jsonb(revision.generation)=v_record->'originGeneration'
                AND invocation.operation=v_record->>'operation' AND invocation.effect_key=v_record->>'effectKey'
                AND invocation.input_digest=v_record->>'inputDigest' AND invocation.definition_digest=v_record->>'definitionDigest'
                AND invocation.resource_scope_digest=v_record->>'scopeDigest' AND invocation.approval_policy=v_record->>'approvalPolicy'
                AND to_jsonb(invocation.grant_version)=v_record->'grantVersion'
                AND step.value->>'Key'=NEW.workflow_step_key AND requirement.value->>'Key'=v_record->>'resultKey'
                AND requirement.value->>'Repository'=(invocation.resource_scope->>'owner')||'/'||(invocation.resource_scope->>'repository')
                AND ((requirement.value->>'Kind'='PULL_REQUEST' AND invocation.operation IN ('github.repository.bundle.publish','github.pull_request.create'))
                    OR (requirement.value->>'Kind'='INTEGRATION_RECEIPT' AND requirement.value->>'Operation'='github.pull_request.merge' AND invocation.operation='github.pull_request.merge'))
                AND EXISTS (SELECT 1 FROM jsonb_array_elements(COALESCE(revision.safe_snapshot->'integrationGrants','[]'::jsonb)) pin
                    WHERE pin->>'ref'=grant_binding.ref AND pin->>'grantVersion'=grant_binding.version::text AND pin->>'connectionRef'=connection.ref
                        AND pin->>'operation'=invocation.operation AND pin->>'definitionDigest'=invocation.definition_digest AND pin->>'approvalPolicy'=invocation.approval_policy)
        ) THEN RAISE EXCEPTION 'workflow intent requires exact native origin' USING ERRCODE='23514',CONSTRAINT='workflow_effect_intent_origin'; END IF;
        IF (SELECT count(*) FROM jsonb_array_elements(NEW.workflow_effect_intents) item
            WHERE item->>'resultKey'=v_record->>'resultKey' AND item->>'operation'=v_record->>'operation')<>1 THEN
            RAISE EXCEPTION 'workflow logical intent slot already exists' USING ERRCODE='23514',CONSTRAINT='workflow_effect_slot_unique';
        END IF;
    ELSE
        v_record:=NEW.workflow_effect_rejoins->(jsonb_array_length(v_old_rejoins));
        SELECT COALESCE(sum(jsonb_array_length(node.workflow_effect_rejoins)),0) INTO v_count
            FROM control_plane.run_nodes node WHERE node.organization_id=NEW.organization_id AND node.root_run_id=NEW.root_run_id
                AND COALESCE(node.workflow_step_origin_node_id,node.id)=v_anchor AND node.workflow_business_attempt=NEW.workflow_business_attempt AND node.id<>NEW.id;
        IF v_count+jsonb_array_length(NEW.workflow_effect_rejoins)>16 OR (SELECT count(*) FROM jsonb_object_keys(v_record))<>12
           OR v_record->>'schema' IS DISTINCT FROM 'kodex.workflow-effect-rejoin.v1' OR NOT EXISTS (
            SELECT 1 FROM control_plane.run_nodes anchor
            CROSS JOIN LATERAL jsonb_array_elements(anchor.workflow_effect_intents) intent(value)
            JOIN control_plane.integration_invocations invocation ON invocation.ref=intent.value->>'invocationRef' AND invocation.organization_id=anchor.organization_id AND invocation.state='SUCCEEDED'
            JOIN control_plane.integration_effect_receipts receipt ON receipt.id=invocation.effect_receipt_id AND receipt.invocation_id=invocation.id
                AND receipt.organization_id=invocation.organization_id AND receipt.effect_key=invocation.effect_key AND receipt.input_digest=invocation.input_digest
                AND receipt.result_summary=invocation.result_summary
            JOIN control_plane.integration_connections connection ON connection.id=invocation.connection_id AND connection.organization_id=anchor.organization_id
                AND connection.enabled AND connection.state='CONNECTED' AND to_jsonb(connection.version)=intent.value->'connectionVersion'
            JOIN control_plane.integration_grants grant_binding ON grant_binding.id=invocation.grant_id AND grant_binding.organization_id=anchor.organization_id
                AND grant_binding.enabled AND grant_binding.version=invocation.grant_version AND to_jsonb(grant_binding.version)=intent.value->'grantVersion'
                AND grant_binding.resource_scope_digest=invocation.resource_scope_digest AND grant_binding.definition_digest=invocation.definition_digest
            JOIN control_plane.runtime_revisions revision ON revision.id::text=v_record->>'runtimeRevisionId' AND revision.organization_id=NEW.organization_id
                AND revision.node_id=NEW.id AND revision.run_id=NEW.run_id AND revision.root_run_id=NEW.root_run_id
                AND revision.agent_id=NEW.agent_id AND revision.turn_id=NEW.turn_id AND revision.attempt=NEW.attempt
            JOIN control_plane.runtime_leases lease ON lease.id::text=v_record->>'leaseId' AND lease.runtime_revision_id=revision.id
                AND lease.organization_id=revision.organization_id AND lease.node_id=NEW.id AND lease.run_id=NEW.run_id AND lease.generation=revision.generation
                AND lease.state='CLAIMED' AND lease.expires_at>clock_timestamp()
            WHERE anchor.id=v_anchor AND anchor.organization_id=NEW.organization_id AND anchor.root_run_id=NEW.root_run_id
                AND anchor.agent_id=NEW.agent_id AND anchor.workflow_step_key=NEW.workflow_step_key AND NEW.state='RUNNING'
                AND intent.value->>'resultKey'=v_record->>'resultKey' AND intent.value->>'operation'=v_record->>'operation'
                AND intent.value->>'semanticDigest'=v_record->>'semanticDigest' AND invocation.ref=v_record->>'invocationRef'
                AND receipt.ref=v_record->>'receiptRef' AND receipt.response_digest=v_record->>'receiptDigest'
                AND to_jsonb(revision.generation)=v_record->'generation' AND to_jsonb(NEW.attempt)=v_record->'runtimeAttempt'
                AND to_jsonb(NEW.workflow_business_attempt)=v_record->'businessAttempt'
                AND EXISTS (SELECT 1 FROM jsonb_array_elements(COALESCE(revision.safe_snapshot->'integrationGrants','[]'::jsonb)) pin
                    WHERE pin->>'ref'=grant_binding.ref AND pin->>'grantVersion'=grant_binding.version::text AND pin->>'connectionRef'=connection.ref
                        AND pin->>'operation'=invocation.operation AND pin->>'definitionDigest'=invocation.definition_digest AND pin->>'approvalPolicy'=invocation.approval_policy)
        ) OR EXISTS (SELECT 1 FROM jsonb_array_elements(v_old_rejoins) item WHERE item->>'resultKey'=v_record->>'resultKey' AND item->>'operation'=v_record->>'operation') THEN
            RAISE EXCEPTION 'workflow rejoin requires exact known success' USING ERRCODE='23514',CONSTRAINT='workflow_effect_rejoin_origin';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_workflow_effect_records() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.guard_workflow_effect_records() TO control_plane_runtime;
CREATE TRIGGER workflow_effect_records_guard BEFORE INSERT OR UPDATE OR DELETE ON control_plane.run_nodes
    FOR EACH ROW EXECUTE FUNCTION control_plane.guard_workflow_effect_records();
RESET ROLE;

-- +goose Down
SELECT 1;
