-- +goose Up
SET LOCAL lock_timeout='8s';
SET LOCAL statement_timeout='30s';

-- Goose-журнал читает исходная migrator identity до SET ROLE; runtime права
-- на служебный журнал не выдаются.
-- +goose StatementBegin
DO $predecessor$
BEGIN
 IF (SELECT max(version_id) FROM public.goose_db_version WHERE is_applied) IS DISTINCT FROM 20261026001300::bigint THEN
    RAISE EXCEPTION 'workflow review evidence requires migration013';
 END IF;
END $predecessor$;
-- +goose StatementEnd
SET ROLE control_plane_owner;

-- Профиль включается только опубликованной версией. Никаких изменений прежних
-- записей, grants, ACL или applied migrations эта forward migration не делает.
-- +goose StatementBegin
DO $precondition$
BEGIN
    IF md5((SELECT prosrc FROM pg_proc WHERE oid='control_plane.guard_workflow_effect_records()'::regprocedure)) IS DISTINCT FROM '74f7d5fd475cb0919b76579335b547a5'
        OR md5((SELECT prosrc FROM pg_proc WHERE oid='control_plane.guard_workflow_attempt_journal()'::regprocedure)) IS DISTINCT FROM '70f0fd55213d39ae7f29567fb6b2200b'
        OR md5((SELECT prosrc FROM pg_proc WHERE oid='control_plane.workflow_public_attempt_results(uuid,uuid)'::regprocedure)) IS DISTINCT FROM '3045149fe411e8acd562c73e8f2600cf' THEN
        RAISE EXCEPTION 'workflow review evidence requires exact migration013 preimage';
    END IF;
END $precondition$;
-- +goose StatementEnd

-- Проверка дополняет существующий native origin guard, не заменяя его grants,
-- lease/generation, root locks, idempotency и scope. S3 report bytes разбирает
-- owner Go в той же транзакции; SQL удостоверяет назначенный captured pin.
-- +goose StatementBegin
CREATE FUNCTION control_plane.workflow_evidence_intent_valid(p_tenant uuid,p_anchor uuid,p_origin uuid,p_invocation uuid,p_record jsonb,p_requirement jsonb)
RETURNS boolean LANGUAGE sql STABLE SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $$
 WITH execution AS MATERIALIZED (
    SELECT origin.*,root.project_id,root.initiated_by,version.spec,revision.ref AS revision_ref,
        revision.safe_snapshot,revision.generation AS revision_generation,invocation.operation,
        invocation.bounded_input,invocation.resource_scope
    FROM control_plane.run_nodes origin
    JOIN control_plane.runs root ON root.id=origin.root_run_id AND root.organization_id=p_tenant
    JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=p_tenant
    JOIN control_plane.runtime_revisions revision ON revision.id::text=p_record->>'originRevisionId'
        AND revision.organization_id=p_tenant AND revision.node_id=origin.id AND revision.run_id=origin.run_id
        AND revision.root_run_id=origin.root_run_id AND revision.turn_id=origin.turn_id AND revision.agent_id=origin.agent_id
        AND revision.attempt=origin.attempt
    JOIN control_plane.integration_invocations invocation ON invocation.id=p_invocation AND invocation.organization_id=p_tenant
        AND invocation.node_id=origin.id AND invocation.run_id=origin.run_id
    WHERE origin.id=p_origin AND origin.organization_id=p_tenant
        AND COALESCE(origin.workflow_step_origin_node_id,origin.id)=p_anchor
        AND version.spec->>'ExecutionMode'='SERVER_DAG_V1' AND version.spec->>'ResultEvidenceProfile'='GITHUB_REVIEW_QA_V1'
 ), subjects AS MATERIALIZED (
    SELECT result.value FROM execution
    CROSS JOIN LATERAL jsonb_array_elements(execution.workflow_inputs) dependency(value)
    CROSS JOIN LATERAL jsonb_array_elements(dependency.value #> '{receipt,results}') result(value)
    WHERE dependency.value->>'stepKey'=p_requirement->>'SubjectStepKey'
        AND dependency.value #>> '{receipt,accepted}'='true' AND result.value->>'kind'='PULL_REQUEST'
        
 )
 SELECT count(*)=1 FROM execution CROSS JOIN subjects
 WHERE (SELECT count(*) FROM subjects)=1
    AND subjects.value->>'repository'=(execution.resource_scope->>'owner')||'/'||(execution.resource_scope->>'repository') AND (
    (p_requirement->>'Kind'='REVIEW' AND execution.operation='github.pull_request.review.create'
        AND NOT p_record ? 'qaReport'
        AND execution.bounded_input-ARRAY['pull_request_number','sha','event','body']='{}'::jsonb
        AND execution.bounded_input->'pull_request_number'=subjects.value->'number'
        AND execution.bounded_input->>'sha'=subjects.value->>'headSha'
        AND execution.bounded_input->>'event'='COMMENT' AND jsonb_typeof(execution.bounded_input->'body')='string'
        AND length(btrim(execution.bounded_input->>'body'))>0)
    OR (p_requirement->>'Kind'='QA' AND execution.operation='github.issue.create'
        AND execution.bounded_input-ARRAY['title','body']='{}'::jsonb
        AND jsonb_typeof(execution.bounded_input->'title')='string' AND length(btrim(execution.bounded_input->>'title'))>0
        AND jsonb_typeof(execution.bounded_input->'body')='string' AND length(btrim(execution.bounded_input->>'body'))>0
        AND jsonb_typeof(p_record->'qaReport')='object'
        AND (SELECT count(*) FROM jsonb_object_keys(p_record->'qaReport'))=4
        AND p_record->'qaReport'-ARRAY['ref','revisionRef','revision','digest']='{}'::jsonb
        AND EXISTS (
            SELECT 1 FROM control_plane.workflow_captured_artifact_pins(p_tenant,execution.id) pin
            JOIN control_plane.artifact_history history ON history.id=pin.artifact_id AND history.revision_id=pin.revision_id
                AND history.organization_id=p_tenant AND history.project_id=execution.project_id AND history.run_id=execution.run_id
                AND history.node_id<>execution.id AND history.ref=p_record #>> '{qaReport,ref}'
                AND history.revision_ref=p_record #>> '{qaReport,revisionRef}'
                AND to_jsonb(history.revision)=p_record #> '{qaReport,revision}' AND history.digest=p_record #>> '{qaReport,digest}'
            JOIN control_plane.artifact_heads head ON head.id=history.id AND head.organization_id=p_tenant AND head.lifecycle_state='ACTIVE'
            JOIN control_plane.runtime_file_catalogs catalog ON catalog.organization_id=p_tenant AND catalog.node_id=execution.id
                AND catalog.run_id=execution.run_id AND catalog.project_id=execution.project_id AND catalog.agent_id=execution.agent_id
                AND catalog.actor_id=execution.initiated_by AND catalog.turn_id=execution.turn_id
                AND catalog.runtime_revision_ref=execution.revision_ref AND catalog.generation=execution.revision_generation AND catalog.frozen
                AND catalog.ref=execution.safe_snapshot #>> '{fileCatalog,ref}' AND catalog.digest=execution.safe_snapshot #>> '{fileCatalog,digest}'
                AND to_jsonb(catalog.total)=execution.safe_snapshot #> '{fileCatalog,total}'
                AND to_jsonb(catalog.purposes)=execution.safe_snapshot #> '{fileCatalog,purposes}'
            JOIN control_plane.runtime_file_visible_entries entry ON entry.catalog_id=catalog.id AND entry.purpose='RUN_RESULT'
                AND entry.artifact_id=history.id AND entry.artifact_ref=history.ref AND entry.artifact_revision=history.revision
                AND entry.artifact_digest=history.digest AND entry.file_name=history.file_name AND entry.media_type=history.media_type
                AND entry.size_bytes=history.size_bytes
            CROSS JOIN LATERAL jsonb_array_elements(execution.spec->'Steps') step(value)
            CROSS JOIN LATERAL jsonb_array_elements(step.value->'ResultRequirements') report(value)
            WHERE step.value->>'Key'=execution.workflow_step_key AND report.value->>'Key'=p_requirement->>'EvidenceKey'
                AND report.value->>'Kind'='ARTIFACT' AND report.value->>'FileName'=history.file_name
                AND report.value->>'MediaType'='application/json' AND history.media_type='application/json'
                AND history.lifecycle_state='ACTIVE' AND control_plane.artifact_effective_scan(history.revision_id)='CLEAN'
        ))
 );
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.workflow_evidence_intent_valid(uuid,uuid,uuid,uuid,jsonb,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.workflow_evidence_intent_valid(uuid,uuid,uuid,uuid,jsonb,jsonb) TO control_plane_runtime;

-- Точные native pins прежнего guard сохраняются. Только два новых opt-in slots
-- получают отдельную identity business attempt; остальные logical slots прежние.
-- +goose StatementBegin
DO $effect_guard$
DECLARE v_source text;v_updated text;v_old text;v_new text;
BEGIN
 SELECT prosrc INTO STRICT v_source FROM pg_proc WHERE oid='control_plane.guard_workflow_effect_records()'::regprocedure;
 IF md5(v_source)<>'74f7d5fd475cb0919b76579335b547a5' THEN RAISE EXCEPTION 'workflow evidence effect preimage changed'; END IF;
 v_old:=$old$(SELECT count(*) FROM jsonb_object_keys(v_record))<>21$old$;
 v_new:=$new$((SELECT count(*) FROM jsonb_object_keys(v_record))<>21 AND NOT (
            v_record->>'operation'='github.issue.create' AND v_record ? 'qaReport'
            AND (SELECT count(*) FROM jsonb_object_keys(v_record))=22))$new$;
 IF (length(v_source)-length(replace(v_source,v_old,'')))/length(v_old)<>1 THEN RAISE EXCEPTION 'workflow evidence shape patch is ambiguous'; END IF;
 v_updated:=replace(v_source,v_old,v_new);
 v_old:=$old$AND requirement.value->>'Repository'=(invocation.resource_scope->>'owner')||'/'||(invocation.resource_scope->>'repository')
                AND ((requirement.value->>'Kind'='PULL_REQUEST' AND invocation.operation IN ('github.repository.bundle.publish','github.pull_request.create'))
                    OR (requirement.value->>'Kind'='INTEGRATION_RECEIPT' AND requirement.value->>'Operation'='github.pull_request.merge' AND invocation.operation='github.pull_request.merge'))$old$;
 v_new:=$new$AND ((requirement.value->>'Repository'=(invocation.resource_scope->>'owner')||'/'||(invocation.resource_scope->>'repository')
                    AND ((requirement.value->>'Kind'='PULL_REQUEST' AND invocation.operation IN ('github.repository.bundle.publish','github.pull_request.create'))
                    OR (requirement.value->>'Kind'='INTEGRATION_RECEIPT' AND requirement.value->>'Operation'='github.pull_request.merge' AND invocation.operation='github.pull_request.merge')))
                    OR control_plane.workflow_evidence_intent_valid(NEW.organization_id,NEW.id,origin.id,invocation.id,v_record,requirement.value))$new$;
 IF (length(v_updated)-length(replace(v_updated,v_old,'')))/length(v_old)<>1 THEN RAISE EXCEPTION 'workflow evidence operation patch is ambiguous'; END IF;
 v_updated:=replace(v_updated,v_old,v_new);
 v_old:=$old$WHERE item->>'resultKey'=v_record->>'resultKey' AND item->>'operation'=v_record->>'operation')<>1$old$;
 v_new:=$new$WHERE item->>'resultKey'=v_record->>'resultKey' AND item->>'operation'=v_record->>'operation'
                AND (v_record->>'operation' NOT IN ('github.pull_request.review.create','github.issue.create')
                    OR item->'originBusinessAttempt'=v_record->'originBusinessAttempt'))<>1$new$;
 IF (length(v_updated)-length(replace(v_updated,v_old,'')))/length(v_old)<>1 THEN RAISE EXCEPTION 'workflow evidence slot patch is ambiguous'; END IF;
 v_updated:=replace(v_updated,v_old,v_new);
 v_old:=$old$AND intent.value->>'semanticDigest'=v_record->>'semanticDigest' AND invocation.ref=v_record->>'invocationRef'$old$;
 v_new:=$new$AND intent.value->>'semanticDigest'=v_record->>'semanticDigest' AND invocation.ref=v_record->>'invocationRef'
                AND (invocation.operation NOT IN ('github.pull_request.review.create','github.issue.create')
                    OR intent.value->'originBusinessAttempt'=to_jsonb(NEW.workflow_business_attempt))$new$;
 IF (length(v_updated)-length(replace(v_updated,v_old,'')))/length(v_old)<>1 THEN RAISE EXCEPTION 'workflow evidence bridge patch is ambiguous'; END IF;
 v_updated:=replace(v_updated,v_old,v_new);
 EXECUTE format('CREATE OR REPLACE FUNCTION control_plane.guard_workflow_effect_records() RETURNS trigger LANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog,control_plane AS %L',v_updated);
END $effect_guard$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_workflow_effect_records() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.guard_workflow_effect_records() TO control_plane_runtime;

-- Дополнительный validator вызывается после прежних provenance/cardinality guards.
-- Он не делает отрицательный verdict принятым результатом и не изменяет историю.
-- +goose StatementBegin
CREATE FUNCTION control_plane.workflow_evidence_outcome_valid(p_tenant uuid,p_anchor uuid,p_entry jsonb,p_requirements jsonb)
RETURNS boolean LANGUAGE plpgsql STABLE SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $$
DECLARE v_anchor record;v_spec jsonb;v_facts jsonb;v_fact jsonb;v_requirement jsonb;v_subject jsonb;v_count integer;
BEGIN
 SELECT node.* INTO STRICT v_anchor FROM control_plane.run_nodes node WHERE node.organization_id=p_tenant AND node.id=p_anchor;
 SELECT version.spec INTO STRICT v_spec FROM control_plane.runs root JOIN control_plane.workflow_versions version
    ON version.id=root.workflow_version_id AND version.organization_id=p_tenant WHERE root.id=v_anchor.root_run_id AND root.organization_id=p_tenant;
 IF v_spec->>'ResultEvidenceProfile' IS DISTINCT FROM 'GITHUB_REVIEW_QA_V1'
    OR NOT EXISTS(SELECT 1 FROM jsonb_array_elements(p_requirements) requirement(value) WHERE requirement.value->>'Kind' IN ('REVIEW','QA')) THEN
    RETURN NOT p_entry ? 'reviewEvidence';
 END IF;
 IF p_entry->>'accepted'='true' THEN
    IF p_entry ? 'reviewEvidence' THEN RETURN false; END IF;
    v_facts:=p_entry->'results';
 ELSIF p_entry->>'reason'='CHANGES_REQUIRED' THEN
    IF p_entry->'results'<>'[]'::jsonb THEN RETURN false; END IF;
    v_facts:=p_entry->'reviewEvidence';
 ELSE RETURN NOT p_entry ? 'reviewEvidence';
 END IF;
 IF jsonb_typeof(v_facts) IS DISTINCT FROM 'array' OR jsonb_array_length(v_facts)<>jsonb_array_length(p_requirements)
    OR EXISTS(SELECT 1 FROM jsonb_array_elements(v_facts) fact(value) GROUP BY fact.value->>'key' HAVING count(*)<>1) THEN RETURN false; END IF;
 FOR v_fact IN SELECT value FROM jsonb_array_elements(v_facts) LOOP
    IF jsonb_typeof(v_fact) IS DISTINCT FROM 'object'
        OR v_fact-ARRAY['key','kind','digest','artifactRef','artifactRevision','receiptRef','repository','number','headSha','verdict','targetRef','bundleSha256','imageDigest','healthOk']<>'{}'::jsonb
        OR COALESCE(v_fact->>'digest','')!~'^[a-f0-9]{64}$' THEN RETURN false; END IF;
    SELECT requirement.value INTO v_requirement FROM jsonb_array_elements(p_requirements) requirement(value)
        WHERE requirement.value->>'Key'=v_fact->>'key' AND requirement.value->>'Kind'=v_fact->>'kind';
    IF NOT FOUND THEN RETURN false; END IF;
    IF v_requirement->>'Kind' NOT IN ('REVIEW','QA') THEN CONTINUE; END IF;
    SELECT count(*),min(result.value::text)::jsonb INTO v_count,v_subject
    FROM control_plane.run_nodes physical
    CROSS JOIN LATERAL jsonb_array_elements(physical.workflow_inputs) dependency(value)
    CROSS JOIN LATERAL jsonb_array_elements(dependency.value #> '{receipt,results}') result(value)
    WHERE physical.organization_id=p_tenant AND physical.id::text=p_entry #>> '{provenance,nodeId}'
        AND physical.root_run_id=v_anchor.root_run_id AND physical.agent_id=v_anchor.agent_id
        AND COALESCE(physical.workflow_step_origin_node_id,physical.id)=p_anchor
        AND dependency.value->>'stepKey'=v_requirement->>'SubjectStepKey' AND dependency.value #>> '{receipt,accepted}'='true'
        AND result.value->>'kind'='PULL_REQUEST';
    IF v_count<>1 OR v_subject->>'repository' IS DISTINCT FROM v_fact->>'repository'
        OR v_subject->'number' IS DISTINCT FROM v_fact->'number' OR v_subject->>'headSha' IS DISTINCT FROM v_fact->>'headSha'
        OR COALESCE(v_fact->>'verdict','') NOT IN ('PASS','CHANGES_REQUIRED')
        OR p_entry->>'accepted'='true' AND v_fact->>'verdict'<>'PASS' THEN RETURN false; END IF;
    IF v_requirement->>'Kind'='QA' AND NOT EXISTS (
        SELECT 1 FROM control_plane.workflow_captured_artifact_pins(p_tenant,(p_entry #>> '{provenance,nodeId}')::uuid) pin
        JOIN control_plane.artifact_history history ON history.id=pin.artifact_id AND history.revision_id=pin.revision_id AND history.organization_id=p_tenant
        CROSS JOIN LATERAL jsonb_array_elements(p_requirements) report(value)
        WHERE history.ref=v_fact->>'artifactRef' AND history.revision_ref=v_fact->>'artifactRevision'
            AND history.digest='sha256:'||(v_fact->>'digest') AND history.media_type='application/json'
            AND report.value->>'Key'=v_requirement->>'EvidenceKey' AND report.value->>'Kind'='ARTIFACT'
            AND report.value->>'FileName'=history.file_name AND report.value->>'MediaType'=history.media_type
    ) THEN RETURN false; END IF;
    IF v_requirement->>'Kind'='REVIEW' OR v_fact ? 'receiptRef' THEN
        IF NOT EXISTS (
            SELECT 1 FROM jsonb_array_elements(v_anchor.workflow_effect_intents) intent(value)
            JOIN control_plane.integration_invocations invocation ON invocation.organization_id=p_tenant AND invocation.ref=intent.value->>'invocationRef'
                AND invocation.state='SUCCEEDED' AND invocation.effect_key=intent.value->>'effectKey' AND invocation.input_digest=intent.value->>'inputDigest'
            JOIN control_plane.integration_effect_receipts receipt ON receipt.id=invocation.effect_receipt_id AND receipt.organization_id=p_tenant
                AND receipt.invocation_id=invocation.id AND receipt.effect_key=invocation.effect_key AND receipt.input_digest=invocation.input_digest
                AND receipt.result_summary=invocation.result_summary
            WHERE intent.value->>'resultKey'=v_fact->>'key' AND intent.value->'originBusinessAttempt'=p_entry->'attempt'
                AND receipt.ref=v_fact->>'receiptRef'
                AND (invocation.resource_scope->>'owner')||'/'||(invocation.resource_scope->>'repository')=v_subject->>'repository'
                AND ((v_requirement->>'Kind'='REVIEW' AND invocation.operation='github.pull_request.review.create'
                    AND invocation.bounded_input->'pull_request_number'=v_subject->'number'
                    AND invocation.bounded_input->>'sha'=v_subject->>'headSha' AND invocation.bounded_input->>'event'='COMMENT'
                    AND receipt.result_summary::jsonb->>'state'='COMMENTED' AND receipt.result_summary::jsonb->>'commit_id'=v_subject->>'headSha'
                    AND receipt.result_summary::jsonb->>'body'=invocation.bounded_input->>'body'
                    AND receipt.response_digest=v_fact->>'digest')
                OR (v_requirement->>'Kind'='QA' AND invocation.operation='github.issue.create' AND v_fact->>'verdict'='CHANGES_REQUIRED'
                    AND intent.value #>> '{qaReport,ref}'=v_fact->>'artifactRef'
                    AND intent.value #>> '{qaReport,revisionRef}'=v_fact->>'artifactRevision'
                    AND intent.value #>> '{qaReport,digest}'='sha256:'||(v_fact->>'digest')
                    AND receipt.result_summary::jsonb->>'state'='open'))
        ) THEN RETURN false; END IF;
    END IF;
 END LOOP;
 IF p_entry->>'accepted'='false' AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements(v_facts) fact(value)
    WHERE fact.value->>'kind' IN ('REVIEW','QA') AND fact.value->>'verdict'='CHANGES_REQUIRED') THEN RETURN false; END IF;
 RETURN true;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.workflow_evidence_outcome_valid(uuid,uuid,jsonb,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.workflow_evidence_outcome_valid(uuid,uuid,jsonb,jsonb) TO control_plane_runtime;

-- +goose StatementBegin
DO $journal_guard$
DECLARE v_source text;v_updated text;v_old text;v_new text;
BEGIN
 SELECT prosrc INTO STRICT v_source FROM pg_proc WHERE oid='control_plane.guard_workflow_attempt_journal()'::regprocedure;
 IF md5(v_source)<>'70f0fd55213d39ae7f29567fb6b2200b' THEN RAISE EXCEPTION 'workflow evidence journal preimage changed'; END IF;
 v_old:=$old$'accepted','reason','results','provenance','reworkSubjects']$old$;
 v_new:=$new$'accepted','reason','results','provenance','reworkSubjects','reviewEvidence']$new$;
 IF (length(v_source)-length(replace(v_source,v_old,'')))/length(v_old)<>1 THEN RAISE EXCEPTION 'workflow evidence journal shape patch is ambiguous'; END IF;
 v_updated:=replace(v_source,v_old,v_new);
 v_old:=$old$    RETURN NEW;
END;
$old$;
 v_new:=$new$    IF NOT control_plane.workflow_evidence_outcome_valid(NEW.organization_id,NEW.id,entry,requirements) THEN
        RAISE EXCEPTION 'workflow evidence outcome differs from owner profile' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$new$;
 IF (length(v_updated)-length(replace(v_updated,v_old,'')))/length(v_old)<>1 THEN RAISE EXCEPTION 'workflow evidence journal validator patch is ambiguous'; END IF;
 v_updated:=replace(v_updated,v_old,v_new);
 EXECUTE format('CREATE OR REPLACE FUNCTION control_plane.guard_workflow_attempt_journal() RETURNS trigger LANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog,control_plane AS %L',v_updated);
 SELECT prosrc INTO STRICT v_source FROM pg_proc WHERE oid='control_plane.workflow_public_attempt_results(uuid,uuid)'::regprocedure;
 IF md5(v_source)<>'3045149fe411e8acd562c73e8f2600cf' THEN RAISE EXCEPTION 'workflow evidence public preimage changed'; END IF;
 v_old:=$old$    v_result:=v_result||jsonb_build_array(v_item);$old$;
 v_new:=$new$    IF v_receipt ? 'reviewEvidence' THEN
        v_item:=v_item||jsonb_build_object('evidence',v_receipt->'reviewEvidence');
    END IF;
    v_result:=v_result||jsonb_build_array(v_item);$new$;
 IF (length(v_source)-length(replace(v_source,v_old,'')))/length(v_old)<>1 THEN RAISE EXCEPTION 'workflow evidence public patch is ambiguous'; END IF;
 v_updated:=replace(v_source,v_old,v_new);
 EXECUTE format('CREATE OR REPLACE FUNCTION control_plane.workflow_public_attempt_results(p_tenant uuid,p_node uuid) RETURNS jsonb LANGUAGE plpgsql STABLE SECURITY INVOKER SET search_path=pg_catalog,control_plane AS %L',v_updated);
END $journal_guard$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION control_plane.guard_workflow_attempt_journal() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.guard_workflow_attempt_journal() TO control_plane_runtime;
REVOKE ALL ON FUNCTION control_plane.workflow_public_attempt_results(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.workflow_public_attempt_results(uuid,uuid) TO control_plane_runtime;

-- Только уже завершённый producing epoch двух новых slots остаётся eligible для
-- native worker/join. Active lease либо payload не заменяют historical capture proof.
-- +goose StatementBegin
CREATE FUNCTION control_plane.workflow_evidence_execution_assigned(p_tenant uuid,p_node uuid,p_invocation uuid)
RETURNS boolean LANGUAGE sql STABLE SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $$
 SELECT count(*)=1
 FROM control_plane.run_nodes node
 JOIN control_plane.runs physical ON physical.id=node.run_id AND physical.organization_id=p_tenant
 JOIN control_plane.runs root ON root.id=node.root_run_id AND root.organization_id=p_tenant
    AND root.id=physical.root_run_id AND root.project_id=physical.project_id AND root.state IN ('RUNNING','WAITING_HUMAN')
 JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=p_tenant
 JOIN control_plane.workflows workflow ON workflow.id=version.workflow_id AND workflow.organization_id=p_tenant
    AND workflow.project_id=root.project_id AND workflow.ref=root.target_ref
 JOIN control_plane.projects project ON project.id=root.project_id AND project.organization_id=p_tenant AND project.lifecycle='ACTIVE'
 JOIN control_plane.agents agent ON agent.id=node.agent_id AND agent.organization_id=p_tenant AND agent.project_id=root.project_id AND agent.enabled AND agent.state='READY'
 JOIN control_plane.subjects actor ON actor.id=root.initiated_by AND actor.organization_id=p_tenant AND actor.active
 JOIN control_plane.catalog_access_targets workflow_target ON workflow_target.organization_id=p_tenant AND workflow_target.kind='WORKFLOW' AND workflow_target.id=workflow.id
 JOIN control_plane.catalog_access_targets agent_target ON agent_target.organization_id=p_tenant AND agent_target.kind='AGENT' AND agent_target.id=agent.id
 JOIN control_plane.runtime_revisions revision ON revision.id::text=node.workflow_captured_artifacts->>'runtimeRevisionId'
    AND revision.organization_id=p_tenant AND revision.node_id=node.id AND revision.run_id=physical.id
    AND revision.root_run_id=root.id AND revision.agent_id=agent.id AND revision.turn_id=node.turn_id AND revision.attempt=node.attempt
    AND revision.project_id=project.id AND revision.session_id=physical.session_id
 JOIN control_plane.runtime_leases lease ON lease.id::text=node.workflow_captured_artifacts->>'leaseId'
    AND lease.organization_id=p_tenant AND lease.node_id=node.id AND lease.run_id=physical.id AND lease.runtime_revision_id=revision.id
    AND lease.state='COMPLETED' AND lease.generation=revision.generation AND lease.input_digest=revision.input_digest
    AND to_jsonb(lease.generation)=node.workflow_captured_artifacts->'generation'
 JOIN control_plane.integration_invocations invocation ON invocation.id=p_invocation AND invocation.organization_id=p_tenant
    AND invocation.run_id=physical.id AND invocation.node_id=node.id AND invocation.risk='WRITE'
    AND invocation.operation IN ('github.pull_request.review.create','github.issue.create')
 JOIN control_plane.integration_connections connection ON connection.id=invocation.connection_id AND connection.organization_id=p_tenant
    AND connection.enabled AND connection.state='CONNECTED' AND connection.lifecycle_state='ACTIVE'
    AND connection.definition_version=invocation.definition_version AND connection.definition_digest=invocation.definition_digest
 JOIN control_plane.integration_grants binding ON binding.id=invocation.grant_id AND binding.organization_id=p_tenant
    AND binding.connection_id=connection.id AND binding.target_kind='AGENT' AND binding.target_ref=agent.ref AND binding.enabled
    AND binding.version=invocation.grant_version AND binding.definition_version=invocation.definition_version
    AND binding.definition_digest=invocation.definition_digest AND binding.resource_scope_digest=invocation.resource_scope_digest
    AND binding.approval_policy=invocation.approval_policy AND binding.capability_key=invocation.capability_key
 JOIN control_plane.run_nodes anchor ON anchor.id=COALESCE(node.workflow_step_origin_node_id,node.id)
    AND anchor.organization_id=p_tenant AND anchor.root_run_id=root.id AND anchor.agent_id=node.agent_id
    AND anchor.workflow_step_key=node.workflow_step_key AND anchor.workflow_business_attempt=1 AND anchor.workflow_step_origin_node_id IS NULL
 CROSS JOIN LATERAL jsonb_array_elements(anchor.workflow_effect_intents) intent(value)
 WHERE node.organization_id=p_tenant AND node.id=p_node AND node.state='SUCCEEDED' AND node.finished_at IS NOT NULL
    AND node.owner_rework_gate_id IS NULL AND node.workflow_business_attempt BETWEEN 1 AND 4 AND physical.state='RUNNING'
    AND version.spec->>'ExecutionMode'='SERVER_DAG_V1' AND version.spec->>'ResultEvidenceProfile'='GITHUB_REVIEW_QA_V1'
    AND physical.workflow_version_id=root.workflow_version_id AND physical.target_type IN ('AGENT','AGENT_DELEGATION') AND physical.target_ref=agent.ref
    AND EXISTS(SELECT 1 FROM jsonb_array_elements(version.spec->'Steps') step(value)
        CROSS JOIN LATERAL jsonb_array_elements(step.value->'ResultRequirements') requirement(value)
        WHERE step.value->>'Key'=node.workflow_step_key AND step.value->>'AgentRef'=agent.ref
            AND requirement.value->>'Key'=intent.value->>'resultKey'
            AND ((requirement.value->>'Kind'='REVIEW' AND invocation.operation='github.pull_request.review.create')
                OR (requirement.value->>'Kind'='QA' AND invocation.operation='github.issue.create')))
    AND revision.generation=(SELECT max(latest.generation) FROM control_plane.runtime_revisions latest WHERE latest.node_id=node.id AND latest.organization_id=p_tenant)
    AND intent.value->>'invocationRef'=invocation.ref AND intent.value->>'operation'=invocation.operation
    AND intent.value->>'originNodeId'=node.id::text AND intent.value->>'originRevisionId'=revision.id::text
    AND intent.value->>'originLeaseId'=lease.id::text AND intent.value->'originGeneration'=to_jsonb(lease.generation)
    AND intent.value->'originAttempt'=to_jsonb(node.attempt) AND intent.value->'originBusinessAttempt'=to_jsonb(node.workflow_business_attempt)
    AND control_plane.runtime_execution_before_deadline(p_tenant,physical.id)
    AND control_plane.catalog_resource_visible(p_tenant,actor.id,'workflow.launch','WORKFLOW',workflow_target.id,workflow_target.project_id,workflow_target.owner_id,workflow_target.related_ids,statement_timestamp(),false)
    AND control_plane.catalog_resource_visible(p_tenant,actor.id,'agent.launch','AGENT',agent_target.id,agent_target.project_id,agent_target.owner_id,agent_target.related_ids,statement_timestamp(),false)
    AND control_plane.catalog_resource_visible(p_tenant,actor.id,'integration.manage','INTEGRATION',connection.id,NULL::uuid,connection.created_by,'{}'::jsonb,statement_timestamp(),false)
    AND NOT EXISTS(SELECT 1 FROM control_plane.runtime_leases active WHERE active.node_id=node.id AND active.state='CLAIMED')
    AND NOT EXISTS(SELECT 1 FROM control_plane.owner_gates gate JOIN control_plane.run_nodes gate_node ON gate_node.id=gate.node_id AND gate_node.organization_id=p_tenant
        WHERE gate.organization_id=p_tenant AND gate.root_run_id=root.id AND gate.state='OPEN' AND gate_node.run_id=physical.id)
    AND EXISTS(SELECT 1 FROM jsonb_array_elements(COALESCE(revision.safe_snapshot->'integrationGrants','[]'::jsonb)) frozen
        WHERE frozen->>'ref'=binding.ref AND frozen->>'grantVersion'=binding.version::text AND frozen->>'connectionRef'=connection.ref
            AND frozen->>'operation'=invocation.operation AND frozen->>'definitionDigest'=invocation.definition_digest AND frozen->>'approvalPolicy'=invocation.approval_policy);
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.workflow_evidence_execution_assigned(uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.workflow_evidence_execution_assigned(uuid,uuid,uuid) TO control_plane_runtime;

RESET ROLE;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'workflow review evidence migration is forward-only'; END $$;
-- +goose StatementEnd
