-- +goose Up
SET ROLE control_plane_owner;
SET LOCAL lock_timeout = '8s';
SET LOCAL statement_timeout = '30s';

-- История принадлежит logical anchor, а завершающий lease — точному physical node.
-- Capture удостоверяет происхождение, но не заменяет бизнес-проверку владельца.
-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_workflow_attempt_journal() RETURNS trigger
LANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $$
DECLARE entry jsonb; proof jsonb; requirements jsonb; history_size integer; old_size integer;
BEGIN
    IF TG_OP='INSERT' THEN
        IF NEW.workflow_attempts<>'[]'::jsonb THEN
            RAISE EXCEPTION 'workflow journal must start empty' USING ERRCODE='23514';
        END IF;
        RETURN NEW;
    END IF;
    IF NEW.workflow_attempts IS NOT DISTINCT FROM OLD.workflow_attempts THEN RETURN NEW; END IF;
    IF jsonb_typeof(NEW.workflow_attempts) IS DISTINCT FROM 'array'
       OR NEW.workflow_step_origin_node_id IS NOT NULL OR NEW.workflow_business_attempt<>1 THEN
        RAISE EXCEPTION 'workflow journal belongs to the assigned logical anchor' USING ERRCODE='23514';
    END IF;
    old_size:=jsonb_array_length(OLD.workflow_attempts);
    history_size:=jsonb_array_length(NEW.workflow_attempts);
    IF history_size<>old_size+1 OR history_size>4 OR EXISTS (
        SELECT 1 FROM jsonb_array_elements(OLD.workflow_attempts) WITH ORDINALITY prior(value,position)
        WHERE NEW.workflow_attempts->(prior.position::integer-1) IS DISTINCT FROM prior.value
    ) THEN
        RAISE EXCEPTION 'workflow journal requires one append and an immutable prefix' USING ERRCODE='23514';
    END IF;
    entry:=NEW.workflow_attempts->old_size;
    proof:=entry->'provenance';
    IF jsonb_typeof(entry) IS DISTINCT FROM 'object'
       OR entry-ARRAY['attempt','runRef','runtimeRevisionId','accepted','reason','results','provenance']<>'{}'::jsonb
       OR entry->'attempt' IS DISTINCT FROM to_jsonb(history_size)
       OR jsonb_typeof(entry->'accepted') IS DISTINCT FROM 'boolean'
       OR jsonb_typeof(entry->'results') IS DISTINCT FROM 'array'
       OR jsonb_typeof(entry->'reason') IS DISTINCT FROM 'string'
       OR (entry->>'reason')!~'^[A-Z_]{0,96}$'
       OR jsonb_typeof(proof) IS DISTINCT FROM 'object'
       OR proof-ARRAY['rootRunId','originNodeId','nodeId','leaseId','generation','turnId','runtimeAttempt']<>'{}'::jsonb
       OR proof->>'rootRunId' IS DISTINCT FROM NEW.root_run_id::text
       OR proof->>'originNodeId' IS DISTINCT FROM NEW.id::text
       OR NOT EXISTS (
        SELECT 1 FROM control_plane.run_nodes physical
        JOIN control_plane.runs run ON run.id=physical.run_id AND run.organization_id=physical.organization_id
            AND run.root_run_id=physical.root_run_id
        JOIN control_plane.runtime_revisions revision ON revision.node_id=physical.id
            AND revision.organization_id=physical.organization_id AND revision.run_id=physical.run_id
            AND revision.root_run_id=physical.root_run_id AND revision.turn_id=physical.turn_id
            AND revision.agent_id=physical.agent_id AND revision.attempt=physical.attempt
        JOIN control_plane.runtime_leases lease ON lease.runtime_revision_id=revision.id
            AND lease.organization_id=revision.organization_id AND lease.run_id=revision.run_id
            AND lease.node_id=revision.node_id AND lease.generation=revision.generation AND lease.state='COMPLETED'
        WHERE physical.id::text=proof->>'nodeId' AND physical.organization_id=NEW.organization_id
            AND physical.root_run_id=NEW.root_run_id AND physical.agent_id=NEW.agent_id
            AND physical.workflow_step_key=NEW.workflow_step_key
            AND COALESCE(physical.workflow_step_origin_node_id,physical.id)=NEW.id
            AND physical.workflow_business_attempt=history_size
            AND physical.state IN ('SUCCEEDED','FAILED')
            AND run.ref=entry->>'runRef' AND revision.id::text=entry->>'runtimeRevisionId'
            AND lease.id::text=proof->>'leaseId' AND to_jsonb(lease.generation)=proof->'generation'
            AND physical.turn_id::text=proof->>'turnId' AND to_jsonb(physical.attempt)=proof->'runtimeAttempt'
            AND physical.workflow_captured_artifacts->>'runtimeRevisionId'=revision.id::text
            AND physical.workflow_captured_artifacts->>'leaseId'=lease.id::text
            AND physical.workflow_captured_artifacts->'generation'=to_jsonb(lease.generation)
       ) THEN
        RAISE EXCEPTION 'workflow journal requires exact completed physical provenance' USING ERRCODE='23514';
    END IF;
    SELECT step.value->'ResultRequirements' INTO STRICT requirements
    FROM control_plane.runs root
    JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
    CROSS JOIN LATERAL jsonb_array_elements(version.spec->'Steps') step(value)
    WHERE root.id=NEW.root_run_id AND root.organization_id=NEW.organization_id
        AND version.spec->>'ExecutionMode'='SERVER_DAG_V1' AND step.value->>'Key'=NEW.workflow_step_key;
    IF jsonb_typeof(requirements) IS DISTINCT FROM 'array' THEN
        RAISE EXCEPTION 'workflow journal requires pinned result requirements' USING ERRCODE='23514';
    END IF;
    IF (entry->>'accepted'='true' AND (entry->>'reason'<>'' OR jsonb_array_length(entry->'results')<>jsonb_array_length(requirements)))
       OR (entry->>'accepted'='false' AND (entry->>'reason'='' OR entry->'results'<>'[]'::jsonb))
       OR EXISTS (
        SELECT 1 FROM jsonb_array_elements(entry->'results') result(value)
        GROUP BY result.value->>'key' HAVING count(*)<>1
       ) OR EXISTS (
        SELECT 1 FROM jsonb_array_elements(entry->'results') result(value)
        WHERE jsonb_typeof(result.value) IS DISTINCT FROM 'object' OR NOT EXISTS (
            SELECT 1 FROM jsonb_array_elements(requirements) requirement(value)
            WHERE requirement.value->>'Key'=result.value->>'key' AND requirement.value->>'Kind'=result.value->>'kind'
        )
       ) THEN
        RAISE EXCEPTION 'workflow journal outcome differs from the pinned result contract' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_workflow_attempt_journal() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.guard_workflow_attempt_journal() TO control_plane_runtime;
CREATE TRIGGER workflow_attempt_journal_guard BEFORE INSERT OR UPDATE ON control_plane.run_nodes
    FOR EACH ROW EXECUTE FUNCTION control_plane.guard_workflow_attempt_journal();

-- Snapshot входов назначается один раз при создании нового physical node либо
-- материализации первого anchor. Пустой snapshot также нельзя позднее дополнить.
-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_workflow_input_snapshot() RETURNS trigger
LANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $$
DECLARE input jsonb;
BEGIN
    IF TG_OP='UPDATE' THEN
        IF NEW.workflow_inputs IS NOT DISTINCT FROM OLD.workflow_inputs THEN RETURN NEW; END IF;
        IF OLD.workflow_business_attempt<>0 OR OLD.materialization_state<>'PLANNED'
           OR NEW.workflow_business_attempt<>1 OR NEW.materialization_state<>'MATERIALIZED' THEN
            RAISE EXCEPTION 'workflow physical inputs are immutable after assignment' USING ERRCODE='23514';
        END IF;
    END IF;
    IF jsonb_typeof(NEW.workflow_inputs) IS DISTINCT FROM 'array' OR jsonb_array_length(NEW.workflow_inputs)>8 THEN
        RAISE EXCEPTION 'workflow inputs must be a bounded array' USING ERRCODE='23514';
    END IF;
    IF EXISTS (
        SELECT 1 FROM jsonb_array_elements(NEW.workflow_inputs) item(value)
        GROUP BY item.value->>'stepKey' HAVING count(*)<>1
    ) THEN
        RAISE EXCEPTION 'workflow input dependencies must be unique' USING ERRCODE='23514';
    END IF;
    FOR input IN SELECT value FROM jsonb_array_elements(NEW.workflow_inputs) LOOP
        IF jsonb_typeof(input) IS DISTINCT FROM 'object'
           OR input-ARRAY['stepKey','sourceNodeId','receipt']<>'{}'::jsonb
           OR NOT EXISTS (
            SELECT 1 FROM control_plane.runs root
            JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
            CROSS JOIN LATERAL jsonb_array_elements(version.spec->'Steps') consumer(value)
            JOIN control_plane.run_nodes source ON source.root_run_id=root.id AND source.organization_id=root.organization_id
                AND source.workflow_step_origin_node_id IS NULL AND source.workflow_business_attempt=1
            CROSS JOIN LATERAL jsonb_array_elements(source.workflow_attempts) accepted(value)
            WHERE root.id=NEW.root_run_id AND root.organization_id=NEW.organization_id
                AND version.spec->>'ExecutionMode'='SERVER_DAG_V1'
                AND consumer.value->>'Key'=NEW.workflow_step_key
                AND consumer.value->'DependsOn' ? (source.workflow_step_key)
                AND source.workflow_step_key=input->>'stepKey' AND source.id::text=input->>'sourceNodeId'
                AND accepted.value=input->'receipt' AND accepted.value->>'accepted'='true'
           ) THEN
            RAISE EXCEPTION 'workflow input requires an exact accepted dependency receipt' USING ERRCODE='23514';
        END IF;
    END LOOP;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_workflow_input_snapshot() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.guard_workflow_input_snapshot() TO control_plane_runtime;
CREATE TRIGGER workflow_input_snapshot_guard BEFORE INSERT OR UPDATE ON control_plane.run_nodes
    FOR EACH ROW EXECUTE FUNCTION control_plane.guard_workflow_input_snapshot();

RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    RAISE EXCEPTION 'workflow journal migration is forward-only';
END $$;
-- +goose StatementEnd
