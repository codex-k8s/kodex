-- +goose Up
SET ROLE control_plane_owner;
SET LOCAL lock_timeout = '8s';
SET LOCAL statement_timeout = '30s';

-- PL/pgSQL input конфликтует с runs.input только при непустых зависимостях.
-- Переименование локальной переменной сохраняет OID, ACL и все проверки snapshot.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.guard_workflow_input_snapshot() RETURNS trigger
LANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $$
DECLARE v_input jsonb;
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
    FOR v_input IN SELECT value FROM jsonb_array_elements(NEW.workflow_inputs) LOOP
        IF jsonb_typeof(v_input) IS DISTINCT FROM 'object'
           OR v_input-ARRAY['stepKey','sourceNodeId','receipt']<>'{}'::jsonb
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
                AND source.workflow_step_key=v_input->>'stepKey' AND source.id::text=v_input->>'sourceNodeId'
                AND accepted.value=v_input->'receipt' AND accepted.value->>'accepted'='true'
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

RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    RAISE EXCEPTION 'workflow input guard migration is forward-only';
END $$;
-- +goose StatementEnd
