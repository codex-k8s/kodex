-- +goose Up
-- +goose StatementBegin
DO $predecessor$ BEGIN
 IF (SELECT max(version_id) FROM public.goose_db_version WHERE is_applied) IS DISTINCT FROM 20261026001200::bigint THEN
    RAISE EXCEPTION 'workflow upstream rework requires exact profile012'; END IF;
END $predecessor$;
-- +goose StatementEnd
SET ROLE control_plane_owner;
SET LOCAL lock_timeout='8s';
SET LOCAL statement_timeout='30s';

-- Новый профиль не переписывает старые inputs/journal/Gates и не меняет применённые003..012.
ALTER TABLE control_plane.run_nodes ADD COLUMN workflow_rework_gate_id uuid;
ALTER TABLE control_plane.run_nodes ADD CONSTRAINT workflow_rework_gate_fk
    FOREIGN KEY(workflow_rework_gate_id) REFERENCES control_plane.owner_gates(id) DEFERRABLE INITIALLY IMMEDIATE;
CREATE INDEX workflow_rework_gate_pin ON control_plane.run_nodes(workflow_rework_gate_id) WHERE workflow_rework_gate_id IS NOT NULL;
ALTER TABLE control_plane.owner_gates ADD COLUMN closure_kind text NOT NULL DEFAULT '';
ALTER TABLE control_plane.owner_gates ADD CONSTRAINT owner_gate_closure_kind CHECK (
    closure_kind='' OR (closure_kind='SUPERSEDED' AND state='CANCELLED' AND decision IS NULL));

-- Новый private FK допускается только как одно названное ребро поверх exact011.
-- Fingerprint вычисляется из actual catalog; старое canonical тело подтверждает inverse proof.
-- +goose StatementBegin
DO $purge$
DECLARE v_definition text;v_source text;v_old_edges text;v_edges integer;v_hash text;v_nodes oid[];
BEGIN
 SELECT pg_get_functiondef(function_row.oid),function_row.prosrc INTO STRICT v_definition,v_source FROM pg_proc function_row
 WHERE function_row.oid='control_plane.purge_project_database(uuid,uuid,text)'::regprocedure
    AND pg_get_userbyid(function_row.proowner)='control_plane_owner' AND function_row.prosecdef AND function_row.provolatile='v'
    AND function_row.prorettype='integer'::regtype AND NOT function_row.proretset
    AND function_row.prolang=(SELECT oid FROM pg_language WHERE lanname='plpgsql') AND function_row.proconfig=ARRAY['search_path=pg_catalog']
    AND has_function_privilege('control_plane_runtime',function_row.oid,'EXECUTE')
    AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(function_row.proacl,acldefault('f',function_row.proowner))) acl WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE');
 v_old_edges:=substring(v_source FROM 'IF v_edges <> 286 OR v_edge_fingerprint <> ''[a-f0-9]{32}'' THEN');
 IF v_old_edges IS NULL OR md5(replace(v_source,v_old_edges,'IF v_edges <> 284 OR v_edge_fingerprint <> ''6bf6ad76d8c7ae66e07f75ecf719fdd6'' THEN'))<>'b886c24d4e2f5c4a0ec08123d99462b0' THEN
    RAISE EXCEPTION 'workflow upstream purge preimage changed'; END IF;
 WITH RECURSIVE nodes(relation_id) AS (
    SELECT 'control_plane.projects'::regclass::oid UNION SELECT fk.conrelid FROM nodes JOIN pg_constraint fk ON fk.confrelid=nodes.relation_id
    WHERE fk.contype='f' AND fk.connamespace='control_plane'::regnamespace
 ) SELECT array_agg(relation_id) INTO v_nodes FROM nodes;
 IF (SELECT count(*) FROM pg_class WHERE oid=ANY(v_nodes))<>105 OR
    (SELECT md5(string_agg(namespace.nspname||'.'||relation.relname,E'\n' ORDER BY namespace.nspname,relation.relname)) FROM pg_class relation
        JOIN pg_namespace namespace ON namespace.oid=relation.relnamespace WHERE relation.oid=ANY(v_nodes))<>'f51a0de25bd942a250997775617eeaee' THEN
    RAISE EXCEPTION 'workflow upstream purge nodes changed'; END IF;
 SELECT count(*),md5(string_agg(child_ns.nspname||'.'||child.relname||'|'||fk.conname||'|'||parent_ns.nspname||'.'||parent.relname||'|'||fk.confdeltype::text,E'\n' ORDER BY child_ns.nspname,child.relname,fk.conname))
 INTO v_edges,v_hash FROM pg_constraint fk JOIN pg_class child ON child.oid=fk.conrelid JOIN pg_namespace child_ns ON child_ns.oid=child.relnamespace
 JOIN pg_class parent ON parent.oid=fk.confrelid JOIN pg_namespace parent_ns ON parent_ns.oid=parent.relnamespace
 WHERE fk.contype='f' AND fk.confrelid=ANY(v_nodes) AND fk.conrelid=ANY(v_nodes)
    AND NOT(fk.conrelid='control_plane.run_nodes'::regclass AND fk.conname='workflow_rework_gate_fk');
 IF v_edges<>286 OR v_old_edges IS DISTINCT FROM format('IF v_edges <> %s OR v_edge_fingerprint <> %L THEN',v_edges,v_hash)
    OR NOT EXISTS(SELECT 1 FROM pg_constraint fk WHERE fk.conrelid='control_plane.run_nodes'::regclass AND fk.conname='workflow_rework_gate_fk'
        AND fk.confrelid='control_plane.owner_gates'::regclass AND fk.contype='f' AND fk.condeferrable AND NOT fk.condeferred AND fk.confdeltype='a'
        AND fk.conkey=ARRAY[(SELECT attnum FROM pg_attribute WHERE attrelid='control_plane.run_nodes'::regclass AND attname='workflow_rework_gate_id')]::smallint[]
        AND fk.confkey=ARRAY[(SELECT attnum FROM pg_attribute WHERE attrelid='control_plane.owner_gates'::regclass AND attname='id')]::smallint[]) THEN
    RAISE EXCEPTION 'workflow upstream purge graph composition changed'; END IF;
 SELECT count(*),md5(string_agg(child_ns.nspname||'.'||child.relname||'|'||fk.conname||'|'||parent_ns.nspname||'.'||parent.relname||'|'||fk.confdeltype::text,E'\n' ORDER BY child_ns.nspname,child.relname,fk.conname))
 INTO v_edges,v_hash FROM pg_constraint fk JOIN pg_class child ON child.oid=fk.conrelid JOIN pg_namespace child_ns ON child_ns.oid=child.relnamespace
 JOIN pg_class parent ON parent.oid=fk.confrelid JOIN pg_namespace parent_ns ON parent_ns.oid=parent.relnamespace
 WHERE fk.contype='f' AND fk.confrelid=ANY(v_nodes) AND fk.conrelid=ANY(v_nodes);
 IF v_edges<>287 THEN RAISE EXCEPTION 'workflow upstream purge edge count changed'; END IF;
 EXECUTE replace(v_definition,v_old_edges,format('IF v_edges <> %s OR v_edge_fingerprint <> %L THEN',v_edges,v_hash));
END $purge$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION control_plane.workflow_rework_floors(p_organization uuid,p_root uuid) RETURNS jsonb
LANGUAGE sql STABLE SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $$
    WITH floors AS (
        SELECT member.value->>'stepKey' AS key,max((member.value->>'nextAttempt')::integer) AS attempt
        FROM control_plane.runs root
        JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
        CROSS JOIN LATERAL jsonb_array_elements(root.workflow_exception_history) episode(value)
        JOIN control_plane.owner_gates gate ON gate.id::text=episode.value->>'gateId' AND gate.ref=episode.value->>'gateRef'
            AND gate.organization_id=root.organization_id AND gate.root_run_id=root.id AND gate.project_id=root.project_id
            AND gate.integration_invocation_id IS NULL AND gate.state='APPROVED' AND gate.decision='APPROVE'
        CROSS JOIN LATERAL jsonb_array_elements((episode.value->'reworkPlan'->'targets')||(episode.value->'reworkPlan'->'rechecks')) member(value)
        WHERE root.organization_id=p_organization AND root.id=p_root AND root.id=root.root_run_id
            AND version.spec->>'ExecutionMode'='SERVER_DAG_V1' AND version.spec->>'ReworkPolicy'='OWNER_APPROVED_UPSTREAM_V1'
            AND episode.value->>'kind'='UPSTREAM_REWORK' AND episode.value->>'state' IN ('WAITING_OWNER','RECOVERED')
            AND episode.value->'reworkPlan'->>'schema'='kodex.workflow-upstream-rework.v1'
            AND COALESCE(episode.value->'reworkPlan'->>'blockReason','')=''
        GROUP BY member.value->>'stepKey'
    ) SELECT COALESCE(jsonb_object_agg(key,attempt),'{}'::jsonb) FROM floors;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION control_plane.workflow_effective_receipts(p_organization uuid,p_root uuid) RETURNS jsonb
LANGUAGE plpgsql STABLE SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $$
DECLARE v_spec jsonb; v_step jsonb; v_anchor record; v_receipt jsonb; v_inputs jsonb;
        v_effective jsonb:='{}'::jsonb; v_expected jsonb; v_key text; v_floor jsonb; v_max integer;
BEGIN
    SELECT version.spec INTO v_spec FROM control_plane.runs root
    JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
    JOIN control_plane.workflows workflow ON workflow.id=version.workflow_id AND workflow.organization_id=root.organization_id AND workflow.project_id=root.project_id
    WHERE root.organization_id=p_organization AND root.id=p_root AND root.id=root.root_run_id
        AND version.spec->>'ExecutionMode'='SERVER_DAG_V1' AND version.spec->>'ReworkPolicy'='OWNER_APPROVED_UPSTREAM_V1';
    IF v_spec IS NULL OR jsonb_array_length(v_spec->'Steps')>12 THEN RETURN v_effective; END IF;
    v_floor:=control_plane.workflow_rework_floors(p_organization,p_root);
    FOR v_step IN SELECT value FROM jsonb_array_elements(v_spec->'Steps') LOOP
        v_key:=v_step->>'Key';
        SELECT node.id,node.workflow_attempts INTO v_anchor FROM control_plane.run_nodes node
        WHERE node.organization_id=p_organization AND node.root_run_id=p_root AND node.workflow_step_key=v_key
            AND node.type='AGENT_EXECUTION' AND node.workflow_step_origin_node_id IS NULL;
        IF NOT FOUND THEN RETURN '{}'::jsonb; END IF;
        SELECT max(physical.workflow_business_attempt) INTO v_max FROM control_plane.run_nodes physical
        WHERE physical.organization_id=p_organization AND physical.root_run_id=p_root AND COALESCE(physical.workflow_step_origin_node_id,physical.id)=v_anchor.id;
        v_receipt:=v_anchor.workflow_attempts->-1;
        IF v_max<1 OR jsonb_array_length(v_anchor.workflow_attempts)<>v_max OR v_receipt->>'accepted'<>'true'
           OR (v_receipt->>'attempt')::integer<>v_max OR v_max<COALESCE((v_floor->>v_key)::integer,0)
           OR v_receipt->'provenance'->>'originNodeId' IS DISTINCT FROM v_anchor.id::text THEN CONTINUE; END IF;
        IF EXISTS(SELECT 1 FROM control_plane.owner_gates gate
            JOIN control_plane.run_edges edge ON edge.organization_id=p_organization AND edge.root_run_id=p_root
                AND edge.target_node_id=gate.node_id AND edge.source_node_id::text=v_receipt->'provenance'->>'nodeId' AND edge.type='WAITING_FOR'
            WHERE gate.organization_id=p_organization AND gate.root_run_id=p_root AND gate.state<>'APPROVED') THEN CONTINUE; END IF;
        IF v_step->>'HumanGateAfter'='true' AND NOT EXISTS(SELECT 1 FROM control_plane.owner_gates gate
            JOIN control_plane.run_edges edge ON edge.organization_id=p_organization AND edge.root_run_id=p_root
                AND edge.target_node_id=gate.node_id AND edge.source_node_id::text=v_receipt->'provenance'->>'nodeId' AND edge.type='WAITING_FOR'
            WHERE gate.organization_id=p_organization AND gate.root_run_id=p_root AND gate.state='APPROVED') THEN CONTINUE; END IF;
        SELECT jsonb_agg(DISTINCT physical.workflow_inputs)->0 INTO v_inputs FROM control_plane.run_nodes physical
        WHERE physical.organization_id=p_organization AND physical.root_run_id=p_root
            AND COALESCE(physical.workflow_step_origin_node_id,physical.id)=v_anchor.id AND physical.workflow_business_attempt=v_max
        HAVING count(DISTINCT physical.workflow_inputs)=1;
        SELECT COALESCE(jsonb_agg(jsonb_build_object('stepKey',dependency.key,'sourceNodeId',source.id,'receipt',v_effective->dependency.key) ORDER BY dependency.position),'[]'::jsonb)
            INTO v_expected FROM jsonb_array_elements_text(v_step->'DependsOn') WITH ORDINALITY dependency(key,position)
            JOIN control_plane.run_nodes source ON source.organization_id=p_organization AND source.root_run_id=p_root
                AND source.workflow_step_key=dependency.key AND source.workflow_step_origin_node_id IS NULL
            WHERE v_effective ? dependency.key;
        IF v_inputs=v_expected AND jsonb_array_length(v_expected)=jsonb_array_length(v_step->'DependsOn') THEN
            v_effective:=v_effective||jsonb_build_object(v_key,v_receipt);
        END IF;
    END LOOP;
    RETURN v_effective;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION control_plane.workflow_effective_inputs(p_organization uuid,p_root uuid,p_step text) RETURNS jsonb
LANGUAGE plpgsql STABLE SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $$
DECLARE v_step jsonb; v_effective jsonb; v_inputs jsonb;
BEGIN
    SELECT step.value INTO v_step FROM control_plane.runs root
    JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
    CROSS JOIN LATERAL jsonb_array_elements(version.spec->'Steps') step(value)
    WHERE root.organization_id=p_organization AND root.id=p_root AND root.id=root.root_run_id
        AND version.spec->>'ExecutionMode'='SERVER_DAG_V1' AND version.spec->>'ReworkPolicy'='OWNER_APPROVED_UPSTREAM_V1' AND step.value->>'Key'=p_step;
    IF v_step IS NULL THEN RETURN NULL; END IF;
    v_effective:=control_plane.workflow_effective_receipts(p_organization,p_root);
    SELECT COALESCE(jsonb_agg(jsonb_build_object('stepKey',dependency.key,'sourceNodeId',source.id,'receipt',v_effective->dependency.key) ORDER BY dependency.position),'[]'::jsonb)
    INTO v_inputs FROM jsonb_array_elements_text(v_step->'DependsOn') WITH ORDINALITY dependency(key,position)
    JOIN control_plane.run_nodes source ON source.organization_id=p_organization AND source.root_run_id=p_root
        AND source.workflow_step_key=dependency.key AND source.workflow_step_origin_node_id IS NULL WHERE v_effective ? dependency.key;
    IF jsonb_array_length(v_inputs)<>jsonb_array_length(v_step->'DependsOn') THEN RETURN NULL; END IF;
    RETURN v_inputs;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION control_plane.workflow_rework_author(p_spec jsonb,p_key text,p_path text[] DEFAULT ARRAY[]::text[]) RETURNS text
LANGUAGE plpgsql IMMUTABLE SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $$
DECLARE v_step jsonb; v_requirement jsonb; v_author text; v_next text; v_pr boolean:=false;
BEGIN
    IF cardinality(p_path)>=12 OR p_key=ANY(p_path) THEN RETURN NULL; END IF;
    SELECT value INTO v_step FROM jsonb_array_elements(p_spec->'Steps') WHERE value->>'Key'=p_key;
    IF v_step IS NULL THEN RETURN NULL; END IF;
    FOR v_requirement IN SELECT value FROM jsonb_array_elements(v_step->'ResultRequirements') LOOP
        IF v_requirement->>'Kind' IN ('INTEGRATION_RECEIPT','DEPLOYMENT') THEN RETURN NULL; END IF;
        v_pr:=v_pr OR v_requirement->>'Kind'='PULL_REQUEST';
    END LOOP;
    IF v_pr THEN RETURN p_key; END IF;
    FOR v_requirement IN SELECT value FROM jsonb_array_elements(v_step->'ResultRequirements') WHERE value->>'Kind' IN ('REVIEW','QA') LOOP
        v_next:=control_plane.workflow_rework_author(p_spec,v_requirement->>'SubjectStepKey',p_path||p_key);
        IF v_next IS NULL OR (v_author IS NOT NULL AND v_author<>v_next) THEN RETURN NULL; END IF;
        v_author:=v_next;
    END LOOP;
    RETURN v_author;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION control_plane.workflow_irreversible_effect_exists(p_tenant uuid,p_root uuid,p_origin uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY INVOKER SET search_path=pg_catalog AS $$
 SELECT EXISTS(SELECT 1 FROM control_plane.run_nodes node
 JOIN control_plane.integration_invocations invocation ON invocation.node_id=node.id AND invocation.run_id=node.run_id
    AND invocation.organization_id=p_tenant AND invocation.root_run_id=p_root AND invocation.state='SUCCEEDED'
    AND invocation.operation IN ('github.pull_request.merge','ssh.compose.deploy')
 JOIN control_plane.integration_effect_receipts receipt ON receipt.id=invocation.effect_receipt_id
    AND receipt.organization_id=p_tenant AND receipt.invocation_id=invocation.id
    AND receipt.effect_key=invocation.effect_key AND receipt.input_digest=invocation.input_digest
    AND receipt.result_summary=invocation.result_summary
 WHERE node.organization_id=p_tenant AND node.root_run_id=p_root
    AND COALESCE(node.workflow_step_origin_node_id,node.id)=p_origin AND node.workflow_business_attempt BETWEEN 1 AND 4);
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.workflow_irreversible_effect_exists(uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.workflow_irreversible_effect_exists(uuid,uuid,uuid) TO control_plane_runtime;

-- +goose StatementBegin
CREATE FUNCTION control_plane.workflow_upstream_plan_valid(p_organization uuid,p_root uuid,p_episode jsonb) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $$
DECLARE v_spec jsonb; v_source jsonb; v_subjects jsonb; v_subject jsonb; v_requirement jsonb; v_step jsonb;
        v_author text; v_targets jsonb:='{}'::jsonb; v_affected jsonb:='{}'::jsonb; v_plan jsonb; v_member jsonb;
        v_expected jsonb; v_authors jsonb:='[]'::jsonb; v_rechecks jsonb:='[]'::jsonb; v_block text:='';
        v_anchor record; v_attempt integer; v_manual boolean; v_receipt jsonb; v_effective jsonb;
BEGIN
    SELECT version.spec INTO v_spec FROM control_plane.runs root
    JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
    WHERE root.organization_id=p_organization AND root.id=p_root AND root.id=root.root_run_id
        AND version.spec->>'ExecutionMode'='SERVER_DAG_V1' AND version.spec->>'ReworkPolicy'='OWNER_APPROVED_UPSTREAM_V1';
    v_plan:=p_episode->'reworkPlan';v_manual:=p_episode->>'reason'='OWNER_CHANGES_REQUESTED';
    IF v_spec IS NULL OR p_episode->>'kind'<>'UPSTREAM_REWORK' OR v_plan->>'schema'<>'kodex.workflow-upstream-rework.v1'
        OR v_plan->>'sourceStepKey' IS DISTINCT FROM p_episode->>'stepKey' OR v_plan->'sourceAttempt' IS DISTINCT FROM p_episode->'attempt'
        OR (p_episode->>'attempt')::integer NOT BETWEEN 1 AND 4 THEN RETURN false; END IF;
    SELECT value INTO v_source FROM jsonb_array_elements(v_spec->'Steps') WHERE value->>'Key'=p_episode->>'stepKey';
    IF v_source IS NULL THEN RETURN false; END IF;
    SELECT node.workflow_attempts->((p_episode->>'attempt')::integer-1) INTO v_receipt
    FROM control_plane.run_nodes node WHERE node.id::text=p_episode->>'originNodeId' AND node.organization_id=p_organization
        AND node.root_run_id=p_root AND node.workflow_step_origin_node_id IS NULL AND node.workflow_step_key=p_episode->>'stepKey';
    IF v_manual THEN
        IF NOT EXISTS(SELECT 1 FROM control_plane.owner_gates gate JOIN control_plane.run_edges edge
            ON edge.organization_id=p_organization AND edge.root_run_id=p_root AND edge.target_node_id=gate.node_id
                AND edge.source_node_id::text=p_episode->>'finalNodeId' AND edge.type='WAITING_FOR'
            WHERE gate.organization_id=p_organization AND gate.root_run_id=p_root AND gate.integration_invocation_id IS NULL
                AND gate.state='CHANGES_REQUESTED' AND gate.decision='REQUEST_CHANGES') THEN RETURN false; END IF;
        SELECT COALESCE(jsonb_agg(jsonb_build_object('requirementKey',requirement.value->>'Key','subjectStepKey',requirement.value->>'SubjectStepKey')
            ||CASE WHEN COALESCE(requirement.value->>'SubjectResultKey','')<>'' THEN jsonb_build_object('subjectResultKey',requirement.value->>'SubjectResultKey') ELSE '{}'::jsonb END),'[]'::jsonb)
        INTO v_subjects FROM jsonb_array_elements(v_source->'ResultRequirements') requirement(value)
        WHERE requirement.value->>'Kind' IN ('ARTIFACT_REVIEW','REVIEW','QA');
        IF v_subjects='[]'::jsonb THEN v_targets:=jsonb_build_object(p_episode->>'stepKey',true); END IF;
    ELSE
        v_subjects:=v_receipt->'reworkSubjects';
        IF p_episode->>'reason'<>'CHANGES_REQUIRED' OR v_receipt->>'accepted'<>'false' OR jsonb_typeof(v_subjects) IS DISTINCT FROM 'array'
            OR jsonb_array_length(v_subjects) NOT BETWEEN 1 AND 4 THEN RETURN false; END IF;
    END IF;
    FOR v_subject IN SELECT value FROM jsonb_array_elements(v_subjects) LOOP
        SELECT value INTO v_requirement FROM jsonb_array_elements(v_source->'ResultRequirements')
        WHERE value->>'Key'=v_subject->>'requirementKey' AND value->>'SubjectStepKey'=v_subject->>'subjectStepKey'
            AND COALESCE(value->>'SubjectResultKey','')=COALESCE(v_subject->>'subjectResultKey','');
        IF v_requirement IS NULL THEN RETURN false; END IF;
        IF v_requirement->>'Kind'='ARTIFACT_REVIEW' THEN v_author:=v_requirement->>'SubjectStepKey';
        ELSIF v_requirement->>'Kind' IN ('REVIEW','QA') THEN
            v_author:=control_plane.workflow_rework_author(v_spec,v_requirement->>'SubjectStepKey');
        ELSE RETURN false; END IF;
        IF v_author IS NULL THEN RETURN false; END IF;
        v_targets:=v_targets||jsonb_build_object(v_author,true);
    END LOOP;
    IF (SELECT count(*) FROM jsonb_object_keys(v_targets)) NOT BETWEEN 1 AND 4 THEN RETURN false; END IF;
    v_effective:=control_plane.workflow_effective_receipts(p_organization,p_root);
    FOR v_step IN SELECT value FROM jsonb_array_elements(v_spec->'Steps') LOOP
        IF v_targets ? (v_step->>'Key') THEN
            IF NOT v_effective ? (v_step->>'Key') AND NOT (v_manual AND v_step->>'Key'=p_episode->>'stepKey' AND v_receipt->>'accepted'='true') THEN RETURN false; END IF;
            v_affected:=v_affected||jsonb_build_object(v_step->>'Key',true);
        END IF;
        IF EXISTS(SELECT 1 FROM jsonb_array_elements_text(v_step->'DependsOn') dependency(key) WHERE v_affected ? dependency.key) THEN
            v_affected:=v_affected||jsonb_build_object(v_step->>'Key',true);
        END IF;
        IF NOT v_affected ? (v_step->>'Key') THEN CONTINUE; END IF;
        SELECT node.id,node.workflow_attempts INTO v_anchor FROM control_plane.run_nodes node
        WHERE node.organization_id=p_organization AND node.root_run_id=p_root AND node.workflow_step_key=v_step->>'Key'
            AND node.workflow_step_origin_node_id IS NULL AND node.type='AGENT_EXECUTION';
        IF NOT FOUND THEN RETURN false; END IF;
        SELECT COALESCE(max(node.workflow_business_attempt),0) INTO v_attempt FROM control_plane.run_nodes node
        WHERE node.organization_id=p_organization AND node.root_run_id=p_root AND COALESCE(node.workflow_step_origin_node_id,node.id)=v_anchor.id;
        v_member:=jsonb_build_object('stepKey',v_step->>'Key','originNodeId',v_anchor.id,'previousAttempt',v_attempt,'nextAttempt',v_attempt+1);
        IF v_targets ? (v_step->>'Key') THEN v_authors:=v_authors||jsonb_build_array(v_member);ELSE v_rechecks:=v_rechecks||jsonb_build_array(v_member);END IF;
        IF v_attempt>=4 AND v_block='' THEN v_block:='WORKFLOW_ATTEMPTS_EXHAUSTED'; END IF;
        IF control_plane.workflow_irreversible_effect_exists(p_organization,p_root,v_anchor.id) OR EXISTS(SELECT 1 FROM jsonb_array_elements(v_anchor.workflow_attempts) receipt(value)
            CROSS JOIN LATERAL jsonb_array_elements(receipt.value->'results') result(value)
            WHERE receipt.value->>'accepted'='true' AND result.value->>'kind' IN ('INTEGRATION_RECEIPT','DEPLOYMENT')) THEN
            v_block:='WORKFLOW_IRREVERSIBLE_EFFECT_REPAIR_REQUIRED';
        END IF;
    END LOOP;
    v_expected:=jsonb_build_object('schema','kodex.workflow-upstream-rework.v1','sourceStepKey',p_episode->>'stepKey',
        'sourceAttempt',(p_episode->>'attempt')::integer,'targets',v_authors,'rechecks',v_rechecks)
        ||CASE WHEN v_block<>'' THEN jsonb_build_object('blockReason',v_block) ELSE '{}'::jsonb END;
    RETURN v_plan=v_expected;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_workflow_rework_node() RETURNS trigger
LANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $$
DECLARE v_root record; v_parent record; v_valid boolean;
BEGIN
    IF TG_OP='DELETE' THEN
        IF OLD.workflow_rework_gate_id IS NOT NULL AND (NOT control_plane.project_purge_authorized() OR NOT EXISTS (
            SELECT 1 FROM control_plane.project_purge_targets target JOIN control_plane.project_purge_context context USING(transaction_id)
            WHERE target.transaction_id=pg_current_xact_id_if_assigned() AND context.backend_pid=pg_backend_pid()
                AND target.relation_id='control_plane.run_nodes'::regclass AND target.row_key=jsonb_build_array(OLD.id)
        )) THEN RAISE EXCEPTION 'workflow rework pin requires authorized purge' USING ERRCODE='23514'; END IF;
        RETURN OLD;
    END IF;
    IF TG_OP='UPDATE' THEN
        IF NEW.workflow_rework_gate_id IS DISTINCT FROM OLD.workflow_rework_gate_id THEN
            RAISE EXCEPTION 'workflow rework approval pin is immutable' USING ERRCODE='23514';
        END IF;
        IF NOT (OLD.materialization_state='PLANNED' AND NEW.materialization_state='MATERIALIZED') THEN RETURN NEW; END IF;
    END IF;
    SELECT root.state,root.workflow_exception,version.spec INTO v_root FROM control_plane.runs root
    JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
    WHERE root.organization_id=NEW.organization_id AND root.id=NEW.root_run_id AND root.id=root.root_run_id;
    IF NEW.workflow_rework_gate_id IS NOT NULL AND (v_root.spec->>'ReworkPolicy' IS DISTINCT FROM 'OWNER_APPROVED_UPSTREAM_V1'
        OR v_root.spec->>'ExecutionMode' IS DISTINCT FROM 'SERVER_DAG_V1' OR NEW.workflow_business_attempt NOT BETWEEN 2 AND 4) THEN
        RAISE EXCEPTION 'workflow rework pin requires the explicit published profile' USING ERRCODE='23514';
    END IF;
    IF NEW.workflow_business_attempt=0 OR v_root.spec->>'ReworkPolicy' IS DISTINCT FROM 'OWNER_APPROVED_UPSTREAM_V1' THEN RETURN NEW; END IF;
    IF v_root.state NOT IN ('RUNNING','WAITING_HUMAN') THEN RAISE EXCEPTION 'workflow rework requires an active root' USING ERRCODE='23514'; END IF;
    IF NEW.attempt>1 THEN
        SELECT node.workflow_rework_gate_id,node.workflow_inputs INTO v_parent FROM control_plane.run_nodes node
        WHERE node.id=NEW.parent_node_id AND node.organization_id=NEW.organization_id AND node.root_run_id=NEW.root_run_id
            AND node.run_id=NEW.run_id AND node.agent_id=NEW.agent_id AND node.workflow_step_key=NEW.workflow_step_key
            AND node.workflow_business_attempt=NEW.workflow_business_attempt AND node.attempt=NEW.attempt-1
            AND node.state='SUCCEEDED' AND node.workflow_captured_artifacts IS NOT NULL;
        IF NOT FOUND OR NEW.workflow_rework_gate_id IS DISTINCT FROM v_parent.workflow_rework_gate_id
            OR NEW.workflow_inputs IS DISTINCT FROM v_parent.workflow_inputs THEN
            RAISE EXCEPTION 'workflow continuation must inherit exact rework pins' USING ERRCODE='23514';
        END IF;
        RETURN NEW;
    END IF;
    IF NEW.workflow_inputs IS DISTINCT FROM control_plane.workflow_effective_inputs(NEW.organization_id,NEW.root_run_id,NEW.workflow_step_key) THEN
        RAISE EXCEPTION 'workflow fresh execution requires current accepted inputs' USING ERRCODE='23514';
    END IF;
    IF NEW.workflow_rework_gate_id IS NULL THEN RETURN NEW; END IF;
    SELECT EXISTS(SELECT 1 FROM control_plane.runs root
        CROSS JOIN LATERAL jsonb_array_elements(root.workflow_exception_history) episode(value)
        JOIN control_plane.owner_gates gate ON gate.id=NEW.workflow_rework_gate_id AND gate.id::text=episode.value->>'gateId'
            AND gate.ref=episode.value->>'gateRef' AND gate.organization_id=NEW.organization_id AND gate.root_run_id=NEW.root_run_id
            AND gate.project_id=root.project_id AND gate.integration_invocation_id IS NULL AND gate.state='APPROVED' AND gate.decision='APPROVE'
        CROSS JOIN LATERAL jsonb_array_elements((episode.value->'reworkPlan'->'targets')||(episode.value->'reworkPlan'->'rechecks')) member(value)
        WHERE root.organization_id=NEW.organization_id AND root.id=NEW.root_run_id
            AND episode.value->>'kind'='UPSTREAM_REWORK' AND episode.value->>'state' IN ('WAITING_OWNER','RECOVERED')
            AND COALESCE(episode.value->'reworkPlan'->>'blockReason','')=''
            AND member.value->>'stepKey'=NEW.workflow_step_key AND member.value->>'originNodeId'=NEW.workflow_step_origin_node_id::text
            AND member.value->'nextAttempt'=to_jsonb(NEW.workflow_business_attempt)
            AND member.value->'previousAttempt'=to_jsonb(NEW.workflow_business_attempt-1)) INTO v_valid;
    IF NOT v_valid THEN
        -- Обычный business retry наследует context pin, но его authority остаётся новым ordinary recovery Gate.
        SELECT EXISTS(SELECT 1 FROM control_plane.run_nodes previous
            JOIN control_plane.owner_gates recovery ON recovery.id::text=v_root.workflow_exception->>'gateId'
                AND recovery.organization_id=NEW.organization_id AND recovery.root_run_id=NEW.root_run_id
                AND recovery.integration_invocation_id IS NULL AND recovery.state='APPROVED' AND recovery.decision='APPROVE'
            WHERE previous.organization_id=NEW.organization_id AND previous.root_run_id=NEW.root_run_id
                AND COALESCE(previous.workflow_step_origin_node_id,previous.id)=NEW.workflow_step_origin_node_id
                AND previous.id::text=v_root.workflow_exception->>'finalNodeId'
                AND previous.workflow_business_attempt=NEW.workflow_business_attempt-1
                AND previous.workflow_rework_gate_id=NEW.workflow_rework_gate_id AND previous.workflow_inputs=NEW.workflow_inputs
                AND v_root.workflow_exception->>'state'='WAITING_OWNER' AND NOT v_root.workflow_exception ? 'kind'
                AND v_root.workflow_exception->>'stepKey'=NEW.workflow_step_key
                AND v_root.workflow_exception->'attempt'=to_jsonb(NEW.workflow_business_attempt-1)) INTO v_valid;
    END IF;
    IF NOT v_valid THEN RAISE EXCEPTION 'workflow rework approval does not cover the execution' USING ERRCODE='23514'; END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER workflow_rework_node_guard BEFORE INSERT OR UPDATE OR DELETE ON control_plane.run_nodes
    FOR EACH ROW EXECUTE FUNCTION control_plane.guard_workflow_rework_node();

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_workflow_superseded_gate() RETURNS trigger
LANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $$
BEGIN
    IF TG_OP='INSERT' THEN
        IF NEW.closure_kind<>'' THEN RAISE EXCEPTION 'new gate cannot carry a closure' USING ERRCODE='23514'; END IF;
        RETURN NEW;
    END IF;
    IF OLD.closure_kind<>'' AND NEW IS DISTINCT FROM OLD THEN
        RAISE EXCEPTION 'superseded gate history is immutable' USING ERRCODE='23514';
    END IF;
    IF NEW.closure_kind IS NOT DISTINCT FROM OLD.closure_kind THEN RETURN NEW; END IF;
    IF OLD.state<>'OPEN' OR NEW.state<>'CANCELLED' OR NEW.closure_kind<>'SUPERSEDED' OR NEW.decision IS NOT NULL
        OR NEW.version<>OLD.version+1 OR NEW.resolved_at IS NULL OR NEW.resolved_by IS NULL
        OR ROW(NEW.id,NEW.ref,NEW.organization_id,NEW.root_run_id,NEW.project_id,NEW.node_id,NEW.integration_invocation_id)
            IS DISTINCT FROM ROW(OLD.id,OLD.ref,OLD.organization_id,OLD.root_run_id,OLD.project_id,OLD.node_id,OLD.integration_invocation_id)
        OR NEW.integration_invocation_id IS NOT NULL OR NOT EXISTS (
            SELECT 1 FROM control_plane.runs root
            JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
            JOIN control_plane.owner_gates approval ON approval.id::text=root.workflow_exception->>'gateId'
                AND approval.ref=root.workflow_exception->>'gateRef' AND approval.organization_id=root.organization_id
                AND approval.root_run_id=root.id AND approval.project_id=root.project_id AND approval.state='APPROVED'
                AND approval.decision='APPROVE' AND approval.integration_invocation_id IS NULL AND approval.id<>NEW.id
            CROSS JOIN LATERAL jsonb_array_elements((root.workflow_exception->'reworkPlan'->'targets')||(root.workflow_exception->'reworkPlan'->'rechecks')) member(value)
            JOIN control_plane.run_nodes anchor ON anchor.id::text=member.value->>'originNodeId'
                AND anchor.organization_id=root.organization_id AND anchor.root_run_id=root.id
                AND anchor.workflow_step_key=member.value->>'stepKey' AND anchor.workflow_step_origin_node_id IS NULL
            JOIN control_plane.run_nodes producer ON producer.id::text=anchor.workflow_attempts->((member.value->>'previousAttempt')::integer-1)->'provenance'->>'nodeId'
                AND producer.organization_id=root.organization_id AND producer.root_run_id=root.id
                AND COALESCE(producer.workflow_step_origin_node_id,producer.id)=anchor.id
                AND producer.workflow_business_attempt=(member.value->>'previousAttempt')::integer
            JOIN control_plane.run_nodes gate_node ON gate_node.id=NEW.node_id AND gate_node.parent_node_id=producer.id
                AND gate_node.organization_id=root.organization_id AND gate_node.root_run_id=root.id AND gate_node.type='HUMAN_GATE'
            JOIN control_plane.run_edges edge ON edge.organization_id=root.organization_id AND edge.root_run_id=root.id
                AND edge.source_node_id=producer.id AND edge.target_node_id=gate_node.id AND edge.type='WAITING_FOR'
            WHERE root.organization_id=NEW.organization_id AND root.id=NEW.root_run_id AND root.project_id=NEW.project_id
                AND root.id=root.root_run_id AND root.state IN ('RUNNING','WAITING_HUMAN')
                AND version.spec->>'ReworkPolicy'='OWNER_APPROVED_UPSTREAM_V1'
                AND root.workflow_exception->>'kind'='UPSTREAM_REWORK' AND root.workflow_exception->>'state'='WAITING_OWNER'
                AND COALESCE(root.workflow_exception->'reworkPlan'->>'blockReason','')=''
                AND NOT EXISTS(SELECT 1 FROM control_plane.interaction_deliveries delivery
                    WHERE delivery.organization_id=root.organization_id AND delivery.approval_gate_id=NEW.id)
        ) THEN RAISE EXCEPTION 'supersession requires an exact approved upstream plan' USING ERRCODE='23514'; END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER workflow_superseded_gate_guard BEFORE INSERT OR UPDATE ON control_plane.owner_gates
    FOR EACH ROW EXECUTE FUNCTION control_plane.guard_workflow_superseded_gate();

-- +goose StatementBegin
DO $migration$
DECLARE v_source text;v_updated text;
BEGIN
    SELECT prosrc INTO STRICT v_source FROM pg_proc WHERE oid='control_plane.guard_workflow_attempt_journal()'::regprocedure;
    IF md5(v_source)<>'2e03a8faa684d2c16ce8017fe8cef5e7' THEN RAISE EXCEPTION 'workflow rework guard preimage changed'; END IF;
    v_updated:=v_source;
    IF length(v_updated)-length(replace(v_updated,$old$'accepted','reason','results','provenance']$old$,''))<>length($old$'accepted','reason','results','provenance']$old$) THEN RAISE EXCEPTION 'workflow rework guard patch is ambiguous'; END IF;
    v_updated:=replace(v_updated,$old$'accepted','reason','results','provenance']$old$,$new$'accepted','reason','results','provenance','reworkSubjects']$new$);
    IF length(v_updated)-length(replace(v_updated,$old$    RETURN NEW;
END;
$old$,''))<>length($old$    RETURN NEW;
END;
$old$) THEN RAISE EXCEPTION 'workflow rework guard patch is ambiguous'; END IF;
    v_updated:=replace(v_updated,$old$    RETURN NEW;
END;
$old$,$new$    IF entry ? 'reworkSubjects' THEN
        IF entry->>'accepted'<>'false' OR entry->>'reason'<>'CHANGES_REQUIRED'
            OR jsonb_typeof(entry->'reworkSubjects') IS DISTINCT FROM 'array'
            OR jsonb_array_length(entry->'reworkSubjects') NOT BETWEEN 1 AND 4
            OR NOT EXISTS(SELECT 1 FROM control_plane.runs root JOIN control_plane.workflow_versions version
                ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
                WHERE root.id=NEW.root_run_id AND root.organization_id=NEW.organization_id AND version.spec->>'ReworkPolicy'='OWNER_APPROVED_UPSTREAM_V1')
            OR EXISTS(SELECT 1 FROM jsonb_array_elements(entry->'reworkSubjects') subject(value)
                WHERE jsonb_typeof(subject.value) IS DISTINCT FROM 'object'
                    OR subject.value-ARRAY['requirementKey','subjectStepKey','subjectResultKey']<>'{}'::jsonb
                    OR NOT EXISTS(SELECT 1 FROM jsonb_array_elements(requirements) requirement(value)
                        WHERE requirement.value->>'Key'=subject.value->>'requirementKey'
                            AND requirement.value->>'SubjectStepKey'=subject.value->>'subjectStepKey'
                            AND COALESCE(requirement.value->>'SubjectResultKey','')=COALESCE(subject.value->>'subjectResultKey','')
                            AND requirement.value->>'Kind' IN ('ARTIFACT_REVIEW','REVIEW','QA')))
            OR EXISTS(SELECT 1 FROM jsonb_array_elements(entry->'reworkSubjects') subject(value)
                GROUP BY subject.value->>'requirementKey' HAVING count(*)<>1) THEN
            RAISE EXCEPTION 'workflow rejected review subjects differ from pinned policy' USING ERRCODE='23514';
        END IF;
    ELSIF entry->>'reason'='CHANGES_REQUIRED' AND EXISTS(SELECT 1 FROM control_plane.runs root
        JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
        WHERE root.id=NEW.root_run_id AND root.organization_id=NEW.organization_id AND version.spec->>'ReworkPolicy'='OWNER_APPROVED_UPSTREAM_V1') THEN
        RAISE EXCEPTION 'workflow upstream rejection requires typed subjects' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$new$);
    EXECUTE format('CREATE OR REPLACE FUNCTION control_plane.guard_workflow_attempt_journal() RETURNS trigger LANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog,control_plane AS %L',v_updated);
END $migration$;
-- +goose StatementEnd

-- +goose StatementBegin
DO $migration$
DECLARE v_source text;v_updated text;
BEGIN
    SELECT prosrc INTO STRICT v_source FROM pg_proc WHERE oid='control_plane.guard_workflow_exception_episodes()'::regprocedure;
    IF md5(v_source)<>'424c9bc2a9788805027ea226ad5b4acc' THEN RAISE EXCEPTION 'workflow rework guard preimage changed'; END IF;
    v_updated:=v_source;
    IF length(v_updated)-length(replace(v_updated,$old$'managerNodeId','managerNodeRef','gateId','gateRef','recoveryNodeId','inputs']$old$,''))<>length($old$'managerNodeId','managerNodeRef','gateId','gateRef','recoveryNodeId','inputs']$old$) THEN RAISE EXCEPTION 'workflow rework guard patch is ambiguous'; END IF;
    v_updated:=replace(v_updated,$old$'managerNodeId','managerNodeRef','gateId','gateRef','recoveryNodeId','inputs']$old$,$new$'managerNodeId','managerNodeRef','gateId','gateRef','recoveryNodeId','inputs','kind','reworkPlan']$new$);
    IF length(v_updated)-length(replace(v_updated,$old$    IF new_count=old_count+1 THEN$old$,''))<>length($old$    IF new_count=old_count+1 THEN$old$) THEN RAISE EXCEPTION 'workflow rework guard patch is ambiguous'; END IF;
    v_updated:=replace(v_updated,$old$    IF new_count=old_count+1 THEN$old$,$new$    IF (episode ? 'kind') IS DISTINCT FROM (episode ? 'reworkPlan')
        OR (episode ? 'kind' AND (episode->>'kind'<>'UPSTREAM_REWORK' OR published->>'ReworkPolicy' IS DISTINCT FROM 'OWNER_APPROVED_UPSTREAM_V1')) THEN
        RAISE EXCEPTION 'workflow rework requires the explicit published profile' USING ERRCODE='23514';
    END IF;
    IF new_count=old_count+1 AND episode ? 'kind' AND control_plane.workflow_upstream_plan_valid(NEW.organization_id,NEW.id,episode) IS NOT TRUE THEN
        RAISE EXCEPTION 'workflow upstream plan differs from exact published subjects' USING ERRCODE='23514';
    END IF;
    IF new_count=old_count+1 THEN$new$);
    IF length(v_updated)-length(replace(v_updated,$old$            AND node.workflow_business_attempt=(episode->>'attempt')::integer+1
            AND COALESCE(node.workflow_step_origin_node_id,node.id)::text=episode->>'originNodeId'
            AND node.workflow_inputs=episode->'inputs' AND (episode->>'attempt')::integer<4$old$,''))<>length($old$            AND node.workflow_business_attempt=(episode->>'attempt')::integer+1
            AND COALESCE(node.workflow_step_origin_node_id,node.id)::text=episode->>'originNodeId'
            AND node.workflow_inputs=episode->'inputs' AND (episode->>'attempt')::integer<4$old$) THEN RAISE EXCEPTION 'workflow rework guard patch is ambiguous'; END IF;
    v_updated:=replace(v_updated,$old$            AND node.workflow_business_attempt=(episode->>'attempt')::integer+1
            AND COALESCE(node.workflow_step_origin_node_id,node.id)::text=episode->>'originNodeId'
            AND node.workflow_inputs=episode->'inputs' AND (episode->>'attempt')::integer<4$old$,$new$            AND ((NOT episode ? 'kind'
                AND node.workflow_business_attempt=(episode->>'attempt')::integer+1
                AND COALESCE(node.workflow_step_origin_node_id,node.id)::text=episode->>'originNodeId'
                AND node.workflow_inputs=episode->'inputs' AND (episode->>'attempt')::integer<4)
              OR (episode->>'kind'='UPSTREAM_REWORK' AND node.workflow_rework_gate_id=gate.id
                AND node.workflow_business_attempt BETWEEN 2 AND 4
                AND COALESCE(episode->'reworkPlan'->>'blockReason','')=''
                AND EXISTS(SELECT 1 FROM jsonb_array_elements(episode->'reworkPlan'->'targets') member(value)
                    WHERE member.value->>'originNodeId'=node.workflow_step_origin_node_id::text
                        AND member.value->>'stepKey'=node.workflow_step_key
                        AND member.value->'nextAttempt'=to_jsonb(node.workflow_business_attempt))))$new$);
    EXECUTE format('CREATE OR REPLACE FUNCTION control_plane.guard_workflow_exception_episodes() RETURNS trigger LANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog,control_plane AS %L',v_updated);
END $migration$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.workflow_rework_floors(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.workflow_rework_floors(uuid,uuid) TO control_plane_runtime;
REVOKE ALL ON FUNCTION control_plane.workflow_effective_receipts(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.workflow_effective_receipts(uuid,uuid) TO control_plane_runtime;
REVOKE ALL ON FUNCTION control_plane.workflow_effective_inputs(uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.workflow_effective_inputs(uuid,uuid,text) TO control_plane_runtime;
REVOKE ALL ON FUNCTION control_plane.workflow_rework_author(jsonb,text,text[]) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.workflow_rework_author(jsonb,text,text[]) TO control_plane_runtime;
REVOKE ALL ON FUNCTION control_plane.workflow_upstream_plan_valid(uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.workflow_upstream_plan_valid(uuid,uuid,jsonb) TO control_plane_runtime;
REVOKE ALL ON FUNCTION control_plane.guard_workflow_rework_node() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.guard_workflow_rework_node() TO control_plane_runtime;
REVOKE ALL ON FUNCTION control_plane.guard_workflow_superseded_gate() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.guard_workflow_superseded_gate() TO control_plane_runtime;

-- Новая review-проекция разрешает exact исходный ARTIFACT через immutable accepted journal,
-- а не приписывает reviewer авторство чужого capture. CLEAN и fresh ACL проверяет общий reader.
-- +goose StatementBegin
CREATE FUNCTION control_plane.workflow_reviewed_artifact_pins(p_tenant uuid,p_reviewer uuid,p_result jsonb)
RETURNS TABLE(artifact_id uuid,revision_id uuid)
LANGUAGE sql STABLE SECURITY INVOKER SET search_path=pg_catalog AS $$
 SELECT artifact.id,artifact.revision_id
 FROM control_plane.run_nodes reviewer
 JOIN control_plane.runs root ON root.id=reviewer.root_run_id AND root.organization_id=p_tenant
 JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=p_tenant
 CROSS JOIN LATERAL jsonb_array_elements(version.spec->'Steps') step
 CROSS JOIN LATERAL jsonb_array_elements(step->'ResultRequirements') requirement
 CROSS JOIN LATERAL jsonb_array_elements(reviewer.workflow_inputs) input
 JOIN control_plane.run_nodes source ON source.id::text=input->>'sourceNodeId' AND source.organization_id=p_tenant
     AND source.root_run_id=root.id AND source.workflow_step_origin_node_id IS NULL AND source.workflow_step_key=input->>'stepKey'
 CROSS JOIN LATERAL jsonb_array_elements(source.workflow_attempts) accepted
 JOIN control_plane.run_nodes producer ON producer.id::text=accepted #>> '{provenance,nodeId}'
     AND producer.organization_id=p_tenant AND producer.root_run_id=root.id AND producer.state='SUCCEEDED'
     AND to_jsonb(producer.workflow_business_attempt)=accepted->'attempt'
 CROSS JOIN LATERAL control_plane.workflow_captured_artifact_pins(p_tenant,producer.id) pin
 JOIN control_plane.artifact_history artifact ON artifact.id=pin.artifact_id AND artifact.revision_id=pin.revision_id
     AND artifact.organization_id=p_tenant AND artifact.project_id=root.project_id
 CROSS JOIN LATERAL jsonb_array_elements(accepted->'results') authored
 WHERE reviewer.organization_id=p_tenant AND reviewer.id=p_reviewer AND reviewer.state='SUCCEEDED'
     AND version.spec->>'ExecutionMode'='SERVER_DAG_V1' AND version.spec->>'ReworkPolicy'='OWNER_APPROVED_UPSTREAM_V1'
     AND step->>'Key'=reviewer.workflow_step_key AND requirement->>'Key'=p_result->>'key' AND requirement->>'Kind'='ARTIFACT_REVIEW'
     AND p_result->>'kind'='ARTIFACT_REVIEW' AND p_result->>'verdict'='PASS'
     AND requirement->>'SubjectStepKey'=input->>'stepKey' AND requirement->>'SubjectResultKey'=authored->>'key'
     AND accepted=input->'receipt' AND accepted->>'accepted'='true' AND authored->>'kind'='ARTIFACT'
     AND authored->>'artifactRef'=artifact.ref AND authored->>'artifactRevision'=artifact.revision_ref AND 'sha256:'||(authored->>'digest')=artifact.digest
     AND p_result->>'artifactRef'=artifact.ref AND p_result->>'artifactRevision'=artifact.revision_ref AND 'sha256:'||(p_result->>'digest')=artifact.digest
     AND control_plane.artifact_effective_scan(artifact.revision_id)='CLEAN';
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.workflow_reviewed_artifact_pins(uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.workflow_reviewed_artifact_pins(uuid,uuid,jsonb) TO control_plane_runtime;

-- +goose StatementBegin
CREATE FUNCTION control_plane.workflow_input_artifact_pins(p_tenant uuid,p_root uuid,p_inputs jsonb)
RETURNS TABLE(artifact_id uuid,revision_id uuid)
LANGUAGE sql STABLE SECURITY INVOKER SET search_path=pg_catalog AS $$
 WITH accepted AS MATERIALIZED (
   SELECT root.project_id,journal,producer.id AS producer_id
   FROM control_plane.runs root
   JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=p_tenant
   CROSS JOIN LATERAL jsonb_array_elements(p_inputs) input
   JOIN control_plane.run_nodes source ON source.id::text=input->>'sourceNodeId' AND source.organization_id=p_tenant
     AND source.root_run_id=root.id AND source.workflow_step_origin_node_id IS NULL AND source.workflow_step_key=input->>'stepKey'
   CROSS JOIN LATERAL jsonb_array_elements(source.workflow_attempts) journal
   JOIN control_plane.run_nodes producer ON producer.id::text=journal #>> '{provenance,nodeId}' AND producer.organization_id=p_tenant
     AND producer.root_run_id=root.id AND producer.state='SUCCEEDED' AND to_jsonb(producer.workflow_business_attempt)=journal->'attempt'
   WHERE root.organization_id=p_tenant AND root.id=p_root AND root.id=root.root_run_id
     AND version.spec->>'ReworkPolicy'='OWNER_APPROVED_UPSTREAM_V1' AND version.spec->>'ExecutionMode'='SERVER_DAG_V1'
     AND journal=input->'receipt' AND journal->>'accepted'='true' AND jsonb_array_length(p_inputs)<=8
 ), candidates AS (
   SELECT pin.artifact_id,pin.revision_id FROM accepted
   CROSS JOIN LATERAL jsonb_array_elements(accepted.journal->'results') result
   CROSS JOIN LATERAL control_plane.workflow_captured_artifact_pins(p_tenant,accepted.producer_id) pin
   JOIN control_plane.artifact_history artifact ON artifact.id=pin.artifact_id AND artifact.revision_id=pin.revision_id
     AND artifact.organization_id=p_tenant AND artifact.project_id=accepted.project_id
   WHERE result->>'kind'='ARTIFACT' AND result->>'artifactRef'=artifact.ref
     AND result->>'artifactRevision'=artifact.revision_ref AND 'sha256:'||(result->>'digest')=artifact.digest
   UNION
   SELECT pin.* FROM accepted CROSS JOIN LATERAL jsonb_array_elements(accepted.journal->'results') result
   CROSS JOIN LATERAL control_plane.workflow_reviewed_artifact_pins(p_tenant,accepted.producer_id,result) pin
   WHERE result->>'kind'='ARTIFACT_REVIEW'
 ) SELECT candidates.* FROM candidates
 JOIN control_plane.artifact_revision_content content ON content.revision_id=candidates.revision_id
 WHERE control_plane.artifact_effective_scan(candidates.revision_id)='CLEAN';
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.workflow_input_artifact_pins(uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.workflow_input_artifact_pins(uuid,uuid,jsonb) TO control_plane_runtime;

-- +goose StatementBegin
CREATE FUNCTION control_plane.workflow_rework_artifact_pins(p_tenant uuid,p_node uuid)
RETURNS TABLE(artifact_id uuid,revision_id uuid)
LANGUAGE sql STABLE SECURITY INVOKER SET search_path=pg_catalog AS $$
 WITH assigned AS MATERIALIZED (
   SELECT node.root_run_id,node.workflow_inputs,node.workflow_step_origin_node_id,episode.value,root.project_id,
     member.value->>'originNodeId' AS origin_id,node.workflow_business_attempt-1 AS previous_attempt
   FROM control_plane.run_nodes node
   JOIN control_plane.runs root ON root.id=node.root_run_id AND root.organization_id=p_tenant AND root.id=root.root_run_id
     AND root.state IN ('QUEUED','RUNNING','WAITING_HUMAN')
   JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=p_tenant
   CROSS JOIN LATERAL jsonb_array_elements(root.workflow_exception_history) episode(value)
   JOIN control_plane.owner_gates approval ON approval.id=node.workflow_rework_gate_id AND approval.id::text=episode.value->>'gateId'
     AND approval.ref=episode.value->>'gateRef' AND approval.organization_id=p_tenant AND approval.root_run_id=root.id
     AND approval.project_id=root.project_id AND approval.state='APPROVED' AND approval.decision='APPROVE' AND approval.integration_invocation_id IS NULL
   CROSS JOIN LATERAL jsonb_array_elements((episode.value->'reworkPlan'->'targets')||(episode.value->'reworkPlan'->'rechecks')) member(value)
   WHERE node.organization_id=p_tenant AND node.id=p_node AND control_plane.runtime_workflow_step(p_tenant,p_node)
     AND version.spec->>'ReworkPolicy'='OWNER_APPROVED_UPSTREAM_V1' AND episode.value->>'kind'='UPSTREAM_REWORK'
     AND episode.value->>'state'='RECOVERED' AND COALESCE(episode.value->'reworkPlan'->>'blockReason','')=''
     AND member.value->>'stepKey'=node.workflow_step_key AND member.value->>'originNodeId'=node.workflow_step_origin_node_id::text
     AND (member.value->>'nextAttempt')::integer<=node.workflow_business_attempt
 ), candidates AS MATERIALIZED (
   SELECT pin.* FROM assigned JOIN control_plane.run_nodes source ON source.id::text=assigned.value->>'finalNodeId'
     AND source.organization_id=p_tenant AND source.root_run_id=assigned.root_run_id
   CROSS JOIN LATERAL control_plane.workflow_captured_artifact_pins(p_tenant,source.id) pin
   UNION
   SELECT pin.* FROM assigned JOIN control_plane.run_nodes anchor ON anchor.id::text=assigned.origin_id
     AND anchor.organization_id=p_tenant AND anchor.root_run_id=assigned.root_run_id
   JOIN control_plane.run_nodes previous ON previous.id::text=anchor.workflow_attempts->(assigned.previous_attempt-1)->'provenance'->>'nodeId'
     AND previous.organization_id=p_tenant AND previous.root_run_id=assigned.root_run_id
   CROSS JOIN LATERAL control_plane.workflow_captured_artifact_pins(p_tenant,previous.id) pin
   UNION SELECT pin.* FROM assigned CROSS JOIN LATERAL control_plane.workflow_input_artifact_pins(p_tenant,assigned.root_run_id,assigned.value->'inputs') pin
 ) SELECT candidates.* FROM candidates
 JOIN control_plane.artifact_history artifact ON artifact.id=candidates.artifact_id AND artifact.revision_id=candidates.revision_id AND artifact.organization_id=p_tenant
 JOIN control_plane.artifact_revision_content content ON content.revision_id=artifact.revision_id AND content.digest=artifact.digest AND content.size_bytes=artifact.size_bytes
 WHERE control_plane.artifact_effective_scan(artifact.revision_id)='CLEAN';
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.workflow_rework_artifact_pins(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.workflow_rework_artifact_pins(uuid,uuid) TO control_plane_runtime;

-- OID и existing012 manager branch сохраняются; source preimage закрыто проверяется.
-- +goose StatementBegin
DO $migration$
DECLARE v_source text;v_updated text;v_marker text;
BEGIN
 SELECT function_row.prosrc INTO STRICT v_source FROM pg_proc function_row
 WHERE function_row.oid='control_plane.workflow_runtime_artifact_pins(uuid,uuid)'::regprocedure
    AND md5(function_row.prosrc)='ef7be3e3843ea14eb9e044fded3449fe'
    AND pg_get_userbyid(function_row.proowner)='control_plane_owner' AND NOT function_row.prosecdef
    AND function_row.provolatile='s' AND function_row.proretset AND function_row.proconfig=ARRAY['search_path=pg_catalog']
    AND has_function_privilege('control_plane_runtime',function_row.oid,'EXECUTE')
    AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(function_row.proacl,acldefault('f',function_row.proowner))) acl WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE');
 v_marker:=E' UNION SELECT * FROM control_plane.workflow_exception_artifact_pins(p_tenant,p_node);';
 IF length(v_source)-length(replace(v_source,v_marker,''))<>length(v_marker) THEN
    RAISE EXCEPTION 'workflow upstream reader requires exact exception-context profile'; END IF;
 v_updated:=replace(v_source,v_marker,E' UNION SELECT * FROM control_plane.workflow_exception_artifact_pins(p_tenant,p_node)\n UNION SELECT * FROM control_plane.workflow_rework_artifact_pins(p_tenant,p_node)\n UNION SELECT pin.* FROM consumer CROSS JOIN LATERAL control_plane.workflow_input_artifact_pins(p_tenant,consumer.root_run_id,consumer.workflow_inputs) pin;');
 EXECUTE format('CREATE OR REPLACE FUNCTION control_plane.workflow_runtime_artifact_pins(uuid,uuid) RETURNS TABLE(artifact_id uuid,revision_id uuid) LANGUAGE sql STABLE SECURITY INVOKER SET search_path=pg_catalog AS %L',v_updated);
END $migration$;
-- +goose StatementEnd

-- Public whitelist сохраняет historical accepted как факт, отдельно отмечая current freshness.
-- +goose StatementBegin
CREATE FUNCTION control_plane.workflow_public_attempt_results(p_tenant uuid,p_node uuid) RETURNS jsonb
LANGUAGE plpgsql STABLE SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $$
DECLARE v_anchor record;v_root record;v_effective jsonb;v_floor jsonb;v_max integer;v_receipt jsonb;v_item jsonb;v_state text;v_result jsonb:='[]'::jsonb;
BEGIN
 SELECT node.* INTO v_anchor FROM control_plane.run_nodes node WHERE node.id=p_node AND node.organization_id=p_tenant;
 IF NOT FOUND OR v_anchor.workflow_attempts='[]'::jsonb THEN RETURN v_result; END IF;
 SELECT root.state,version.spec INTO v_root FROM control_plane.runs root JOIN control_plane.workflow_versions version
    ON version.id=root.workflow_version_id AND version.organization_id=p_tenant WHERE root.id=v_anchor.root_run_id AND root.organization_id=p_tenant;
 IF v_root.spec->>'ReworkPolicy'='OWNER_APPROVED_UPSTREAM_V1' THEN
    v_effective:=control_plane.workflow_effective_receipts(p_tenant,v_anchor.root_run_id);
    v_floor:=control_plane.workflow_rework_floors(p_tenant,v_anchor.root_run_id);
    SELECT max(node.workflow_business_attempt) INTO v_max FROM control_plane.run_nodes node WHERE node.organization_id=p_tenant
        AND node.root_run_id=v_anchor.root_run_id AND COALESCE(node.workflow_step_origin_node_id,node.id)=v_anchor.id;
 END IF;
 FOR v_receipt IN SELECT value FROM jsonb_array_elements(v_anchor.workflow_attempts) LOOP
    v_item:=jsonb_build_object('attempt',v_receipt->'attempt','runRef',v_receipt->'runRef','accepted',v_receipt->'accepted','reason',v_receipt->'reason','results',v_receipt->'results');
    IF v_root.spec->>'ReworkPolicy'='OWNER_APPROVED_UPSTREAM_V1' THEN
        v_state:='CURRENT';
        IF v_receipt->'attempt' IS DISTINCT FROM v_anchor.workflow_attempts->-1->'attempt' THEN v_state:='HISTORICAL';
        ELSIF v_root.state IN ('FAILED','CANCELLED') THEN v_state:='BLOCKED';
        ELSIF (v_receipt->>'attempt')::integer<v_max OR (v_receipt->>'attempt')::integer<COALESCE((v_floor->>v_anchor.workflow_step_key)::integer,0) THEN v_state:='REWORK_PENDING';
        ELSIF v_receipt->>'accepted'='true' AND NOT v_effective ? v_anchor.workflow_step_key THEN
            IF EXISTS(SELECT 1 FROM control_plane.owner_gates gate JOIN control_plane.run_edges edge
                ON edge.organization_id=p_tenant AND edge.root_run_id=v_anchor.root_run_id AND edge.target_node_id=gate.node_id
                    AND edge.source_node_id::text=v_receipt #>> '{provenance,nodeId}' AND edge.type='WAITING_FOR'
                WHERE gate.organization_id=p_tenant AND gate.root_run_id=v_anchor.root_run_id AND gate.state='OPEN') THEN v_state:='WAITING_OWNER';
            ELSIF EXISTS(SELECT 1 FROM control_plane.owner_gates gate JOIN control_plane.run_edges edge
                ON edge.organization_id=p_tenant AND edge.root_run_id=v_anchor.root_run_id AND edge.target_node_id=gate.node_id
                    AND edge.source_node_id::text=v_receipt #>> '{provenance,nodeId}' AND edge.type='WAITING_FOR'
                WHERE gate.organization_id=p_tenant AND gate.root_run_id=v_anchor.root_run_id AND gate.state<>'APPROVED') THEN v_state:='BLOCKED';
            ELSE v_state:='STALE_DEPENDENCY'; END IF;
        END IF;
        v_item:=v_item||jsonb_build_object('effectiveState',v_state);
    END IF;
    v_result:=v_result||jsonb_build_array(v_item);
 END LOOP;
 RETURN v_result;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.workflow_public_attempt_results(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.workflow_public_attempt_results(uuid,uuid) TO control_plane_runtime;
RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'workflow upstream rework migration is forward-only'; END $$;
-- +goose StatementEnd
