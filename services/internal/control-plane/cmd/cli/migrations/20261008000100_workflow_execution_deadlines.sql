-- +goose Up
SET ROLE control_plane_owner;
-- Существующие запуски не получают выдуманный исторический first claim.
-- Cutover требует idle owner graph; новые часы назначаются только при claim.
ALTER TABLE control_plane.runs
    ADD COLUMN execution_started_at timestamptz,
    ADD COLUMN execution_deadline_at timestamptz,
    ADD COLUMN execution_timeout_seconds integer,
    ADD COLUMN execution_step_key text;
ALTER TABLE control_plane.runs ADD CONSTRAINT workflow_execution_clock_shape CHECK (
    (execution_started_at IS NULL AND execution_deadline_at IS NULL AND execution_timeout_seconds IS NULL AND execution_step_key IS NULL)
    OR (execution_started_at IS NOT NULL AND execution_deadline_at IS NOT NULL AND execution_timeout_seconds BETWEEN 1 AND 604800
        AND execution_step_key IS NOT NULL AND length(execution_step_key)<=96
        AND execution_deadline_at=execution_started_at+execution_timeout_seconds*interval '1 second')
);
CREATE INDEX workflow_execution_deadline_active ON control_plane.runs(organization_id,execution_deadline_at)
    WHERE execution_deadline_at IS NOT NULL AND state IN ('QUEUED','RUNNING','WAITING_HUMAN','CANCELLING');

-- +goose StatementBegin
CREATE FUNCTION control_plane.protect_workflow_execution_clock() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,control_plane AS $$
DECLARE
    specification jsonb;
    step text;
    seconds integer;
    started timestamptz;
BEGIN
    IF TG_OP='INSERT' THEN
        IF NEW.execution_started_at IS NOT NULL OR NEW.execution_deadline_at IS NOT NULL
           OR NEW.execution_timeout_seconds IS NOT NULL OR NEW.execution_step_key IS NOT NULL THEN
            RAISE EXCEPTION 'workflow execution clock is assigned on first claim';
        END IF;
        RETURN NEW;
    END IF;
    IF OLD.execution_started_at IS NOT NULL THEN
        IF ROW(NEW.execution_started_at,NEW.execution_deadline_at,NEW.execution_timeout_seconds,NEW.execution_step_key,NEW.workflow_version_id)
           IS DISTINCT FROM ROW(OLD.execution_started_at,OLD.execution_deadline_at,OLD.execution_timeout_seconds,OLD.execution_step_key,OLD.workflow_version_id) THEN
            RAISE EXCEPTION 'workflow execution clock is immutable';
        END IF;
        RETURN NEW;
    END IF;
    IF NEW.execution_started_at IS NULL THEN
        IF NEW.execution_deadline_at IS NOT NULL OR NEW.execution_timeout_seconds IS NOT NULL OR NEW.execution_step_key IS NOT NULL THEN
            RAISE EXCEPTION 'workflow execution clock is incomplete';
        END IF;
        RETURN NEW;
    END IF;
    SELECT version.spec INTO STRICT specification FROM control_plane.workflow_versions version
    JOIN control_plane.runs root ON root.workflow_version_id=version.id
    WHERE root.id=NEW.root_run_id AND root.organization_id=NEW.organization_id
      AND root.project_id IS NOT DISTINCT FROM NEW.project_id
      AND NEW.workflow_version_id=version.id;
    IF NEW.id=NEW.root_run_id THEN
        step := ''; seconds := (specification->>'TimeoutSeconds')::integer;
    ELSE
        SELECT node.workflow_step_key INTO STRICT step FROM control_plane.run_nodes node
        WHERE node.run_id=NEW.id AND node.organization_id=NEW.organization_id AND node.type='AGENT_EXECUTION';
        SELECT (item->>'TimeoutSeconds')::integer INTO STRICT seconds
        FROM jsonb_array_elements(specification->'Steps') item WHERE item->>'Key'=step;
    END IF;
    IF seconds<1 OR seconds>(CASE WHEN step='' THEN 604800 ELSE 86400 END)
       OR NEW.state NOT IN ('QUEUED','RUNNING') THEN
        RAISE EXCEPTION 'workflow execution clock source is invalid';
    END IF;
    started := clock_timestamp();
    NEW.execution_started_at := started;
    NEW.execution_timeout_seconds := seconds;
    NEW.execution_step_key := step;
    NEW.execution_deadline_at := started+seconds*interval '1 second';
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER workflow_execution_clock_guard BEFORE INSERT OR UPDATE ON control_plane.runs
FOR EACH ROW EXECUTE FUNCTION control_plane.protect_workflow_execution_clock();

-- +goose StatementBegin
CREATE FUNCTION control_plane.runtime_execution_before_deadline(organization uuid, execution uuid) RETURNS boolean
LANGUAGE sql SET search_path=pg_catalog,control_plane AS $$
WITH RECURSIVE lineage AS (
    SELECT id,parent_run_id,root_run_id,organization_id,project_id,0 AS depth
    FROM control_plane.runs WHERE id=execution AND organization_id=organization
    UNION ALL
    SELECT parent.id,parent.parent_run_id,parent.root_run_id,parent.organization_id,parent.project_id,child.depth+1
    FROM lineage child JOIN control_plane.runs parent ON parent.id=child.parent_run_id
      AND parent.organization_id=child.organization_id AND parent.project_id IS NOT DISTINCT FROM child.project_id
    WHERE child.depth<128
), owners AS (SELECT id FROM lineage UNION SELECT root_run_id FROM lineage)
SELECT EXISTS(SELECT 1 FROM lineage) AND NOT EXISTS (
    SELECT 1 FROM lineage WHERE depth=128 AND parent_run_id IS NOT NULL
) AND NOT EXISTS (
    SELECT 1 FROM lineage child WHERE child.parent_run_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM control_plane.runs parent WHERE parent.id=child.parent_run_id
          AND parent.organization_id=child.organization_id AND parent.project_id IS NOT DISTINCT FROM child.project_id
    )
) AND NOT EXISTS (
    SELECT 1 FROM owners JOIN control_plane.runs run ON run.id=owners.id AND run.organization_id=organization
    WHERE run.execution_deadline_at<=clock_timestamp()
);
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.runtime_execution_before_deadline(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.runtime_execution_before_deadline(uuid,uuid) TO control_plane_runtime;
RESET ROLE;

-- +goose Down
-- Forward-only: устойчивые часы и terminal история не удаляются.
SELECT 1;
