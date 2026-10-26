-- +goose Up
SET ROLE control_plane_owner;
SET LOCAL lock_timeout='8s';
SET LOCAL statement_timeout='30s';

-- Журнал приватен, не добавляет таблицу/FK и не меняет purge graph.
ALTER TABLE control_plane.runs ADD COLUMN workflow_exception_history jsonb NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE control_plane.runs ADD CONSTRAINT workflow_exception_history_bound CHECK (
    jsonb_typeof(workflow_exception_history)='array' AND jsonb_array_length(workflow_exception_history)<=48);

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_workflow_exception_episodes() RETURNS trigger
LANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $$
DECLARE old_count integer; new_count integer; previous jsonb; episode jsonb; immutable_keys text[];
        terminal boolean; published jsonb;
BEGIN
    IF TG_OP='INSERT' THEN
        IF NEW.workflow_exception_history<>'[]'::jsonb OR NEW.workflow_exception IS NOT NULL THEN
            RAISE EXCEPTION 'workflow exception history must start empty' USING ERRCODE='23514';
        END IF;
        RETURN NEW;
    END IF;
    terminal:=NEW.state IN ('FAILED','CANCELLED');
    old_count:=jsonb_array_length(OLD.workflow_exception_history);
    IF terminal AND OLD.workflow_exception IS NOT NULL AND old_count>0 THEN
        episode:=OLD.workflow_exception_history->-1;
        IF episode->>'state' IN ('PENDING_MANAGER','MANAGER_QUEUED','WAITING_OWNER') THEN
            episode:=episode||jsonb_build_object('state',CASE WHEN NEW.state='FAILED' THEN 'REJECTED' ELSE 'CANCELLED' END,
                'version',(episode->>'version')::bigint+1);
            NEW.workflow_exception_history:=jsonb_set(OLD.workflow_exception_history,ARRAY[(old_count-1)::text],episode);
            NEW.workflow_exception:=NULL;
        END IF;
    END IF;
    IF NEW.workflow_exception_history IS NOT DISTINCT FROM OLD.workflow_exception_history
       AND NEW.workflow_exception IS NOT DISTINCT FROM OLD.workflow_exception THEN
        IF NEW.state='SUCCEEDED' AND NEW.workflow_exception IS NOT NULL THEN
            RAISE EXCEPTION 'workflow recovery blocks successful root completion' USING ERRCODE='23514';
        END IF;
        RETURN NEW;
    END IF;
    IF NEW.id IS DISTINCT FROM NEW.root_run_id OR jsonb_typeof(NEW.workflow_exception_history) IS DISTINCT FROM 'array' THEN
        RAISE EXCEPTION 'workflow episodes belong to one root' USING ERRCODE='23514';
    END IF;
    SELECT version.spec INTO published FROM control_plane.workflow_versions version
    JOIN control_plane.workflows workflow ON workflow.id=version.workflow_id AND workflow.organization_id=version.organization_id
    WHERE version.id=NEW.workflow_version_id AND version.organization_id=NEW.organization_id
        AND workflow.project_id=NEW.project_id AND version.spec->>'ExecutionMode'='SERVER_DAG_V1';
    IF published IS NULL THEN RAISE EXCEPTION 'workflow episode requires pinned server policy' USING ERRCODE='23514'; END IF;
    new_count:=jsonb_array_length(NEW.workflow_exception_history);
    IF new_count<1 OR new_count>48 OR new_count<old_count OR new_count>old_count+1 OR EXISTS (
        SELECT 1 FROM jsonb_array_elements(OLD.workflow_exception_history) WITH ORDINALITY item(value,position)
        WHERE item.position<=LEAST(old_count,new_count-1) AND NEW.workflow_exception_history->(item.position::integer-1) IS DISTINCT FROM item.value
    ) THEN RAISE EXCEPTION 'workflow episode prefix is immutable' USING ERRCODE='23514'; END IF;
    episode:=NEW.workflow_exception_history->-1;
    IF jsonb_typeof(episode) IS DISTINCT FROM 'object'
       OR episode-ARRAY['stepKey','attempt','reason','originNodeId','finalNodeId','episode','version','state',
           'managerNodeId','managerNodeRef','gateId','gateRef','recoveryNodeId','inputs']<>'{}'::jsonb
       OR NOT episode ?& ARRAY['stepKey','attempt','reason','originNodeId','finalNodeId','episode','version','state','inputs']
       OR episode->'episode' IS DISTINCT FROM to_jsonb(new_count)
       OR jsonb_typeof(episode->'inputs') IS DISTINCT FROM 'array'
       OR jsonb_array_length(episode->'inputs')>8
       OR jsonb_typeof(episode->'state') IS DISTINCT FROM 'string'
       OR jsonb_typeof(episode->'attempt') IS DISTINCT FROM 'number'
       OR jsonb_typeof(episode->'version') IS DISTINCT FROM 'number'
       OR jsonb_typeof(episode->'reason') IS DISTINCT FROM 'string'
       OR (episode->>'reason')!~'^[A-Z_]{1,96}$'
       OR episode->>'state' NOT IN ('PENDING_MANAGER','MANAGER_QUEUED','WAITING_OWNER','RECOVERED','REJECTED','CANCELLED') THEN
        RAISE EXCEPTION 'workflow episode shape is invalid' USING ERRCODE='23514';
    END IF;
    IF new_count=old_count+1 THEN
        IF old_count>0 AND (OLD.workflow_exception IS NOT NULL OR OLD.workflow_exception_history->-1->>'state' NOT IN ('RECOVERED','REJECTED','CANCELLED')) THEN
            RAISE EXCEPTION 'workflow episode already active' USING ERRCODE='23514';
        END IF;
        IF episode->'version' IS DISTINCT FROM '1'::jsonb OR episode->>'state'<>'PENDING_MANAGER'
           OR episode ?| ARRAY['managerNodeId','managerNodeRef','gateId','gateRef','recoveryNodeId']
           OR (episode->>'attempt')::integer NOT BETWEEN 0 AND 4 OR NOT EXISTS (
            SELECT 1 FROM control_plane.run_nodes anchor
            JOIN control_plane.run_nodes physical ON physical.id::text=episode->>'finalNodeId'
                AND physical.organization_id=anchor.organization_id AND physical.root_run_id=anchor.root_run_id
            WHERE anchor.organization_id=NEW.organization_id AND anchor.root_run_id=NEW.id
                AND anchor.id::text=episode->>'originNodeId' AND anchor.workflow_step_origin_node_id IS NULL
                AND anchor.workflow_step_key=episode->>'stepKey'
                AND ((episode->>'attempt'='0' AND anchor.id=physical.id AND anchor.workflow_business_attempt=0
                    AND anchor.state='PLANNED' AND anchor.materialization_state='PLANNED')
                  OR (episode->>'attempt'<>'0' AND COALESCE(physical.workflow_step_origin_node_id,physical.id)=anchor.id
                    AND physical.workflow_business_attempt=(episode->>'attempt')::integer
                    AND physical.workflow_inputs=episode->'inputs'
                    AND jsonb_array_length(anchor.workflow_attempts)=(episode->>'attempt')::integer
                    AND anchor.workflow_attempts->-1->'provenance'->>'nodeId'=physical.id::text
                    AND (anchor.workflow_attempts->-1->>'reason'=episode->>'reason'
                         OR (episode->>'reason'='OWNER_CHANGES_REQUESTED' AND anchor.workflow_attempts->-1->>'accepted'='true'))))
        ) THEN RAISE EXCEPTION 'workflow episode requires exact current attempt' USING ERRCODE='23514'; END IF;
    ELSE
        previous:=OLD.workflow_exception_history->-1;
        immutable_keys:=ARRAY['version','state','managerNodeId','managerNodeRef','gateId','gateRef','recoveryNodeId'];
        IF episode-immutable_keys IS DISTINCT FROM previous-immutable_keys
           OR episode->'version' IS DISTINCT FROM to_jsonb((previous->>'version')::bigint+1)
           OR NOT ((previous->>'state'='PENDING_MANAGER' AND episode->>'state' IN ('MANAGER_QUEUED','WAITING_OWNER'))
               OR (previous->>'state'='MANAGER_QUEUED' AND episode->>'state'='WAITING_OWNER')
               OR (previous->>'state'='WAITING_OWNER' AND episode->>'state'='RECOVERED')
               OR (terminal AND previous->>'state' IN ('PENDING_MANAGER','MANAGER_QUEUED','WAITING_OWNER')
                   AND episode->>'state' IN ('REJECTED','CANCELLED')))
           OR EXISTS (SELECT 1 FROM unnest(ARRAY['managerNodeId','managerNodeRef','gateId','gateRef','recoveryNodeId']) pin
               WHERE previous ? pin AND episode->pin IS DISTINCT FROM previous->pin) THEN
            RAISE EXCEPTION 'workflow episode transition or pins changed' USING ERRCODE='23514';
        END IF;
    END IF;
    IF episode->>'state' IN ('PENDING_MANAGER','MANAGER_QUEUED','WAITING_OWNER') THEN
        IF NEW.workflow_exception IS DISTINCT FROM episode OR NEW.state NOT IN ('QUEUED','RUNNING','WAITING_HUMAN') THEN
            RAISE EXCEPTION 'workflow active episode pin differs' USING ERRCODE='23514';
        END IF;
    ELSIF NEW.workflow_exception IS NOT NULL THEN RAISE EXCEPTION 'workflow terminal episode remains active' USING ERRCODE='23514'; END IF;
    IF episode ? 'managerNodeId' AND NOT EXISTS (
        SELECT 1 FROM control_plane.run_nodes node
        JOIN control_plane.agents agent ON agent.id=node.agent_id AND agent.organization_id=node.organization_id AND agent.project_id=NEW.project_id
        WHERE node.organization_id=NEW.organization_id AND node.root_run_id=NEW.id
            AND node.id::text=episode->>'managerNodeId' AND node.ref=episode->>'managerNodeRef'
            AND node.workflow_step_key='workflow.coordinator.exception.'||(episode->>'episode')
            AND node.workflow_business_attempt=0 AND agent.ref=published->>'CoordinatorAgentRef'
    ) THEN RAISE EXCEPTION 'workflow manager pin differs' USING ERRCODE='23514'; END IF;
    IF episode->>'state'='WAITING_OWNER' AND episode ? 'managerNodeId' AND NOT EXISTS (
        SELECT 1 FROM control_plane.run_nodes node
        JOIN control_plane.runtime_revisions revision ON revision.node_id=node.id AND revision.organization_id=node.organization_id
            AND revision.run_id=node.run_id AND revision.root_run_id=node.root_run_id AND revision.turn_id=node.turn_id
            AND revision.agent_id=node.agent_id AND revision.attempt=node.attempt
        JOIN control_plane.runtime_leases lease ON lease.runtime_revision_id=revision.id AND lease.organization_id=revision.organization_id
            AND lease.run_id=revision.run_id AND lease.node_id=node.id AND lease.generation=revision.generation AND lease.state='COMPLETED'
        WHERE node.organization_id=NEW.organization_id AND node.root_run_id=NEW.id AND node.id::text=episode->>'managerNodeId'
            AND node.state IN ('SUCCEEDED','FAILED')
    ) THEN RAISE EXCEPTION 'workflow manager is not completed' USING ERRCODE='23514'; END IF;
    IF episode->>'state'='WAITING_OWNER' AND NOT EXISTS (
        SELECT 1 FROM control_plane.owner_gates gate
        JOIN control_plane.run_nodes gate_node ON gate_node.id=gate.node_id AND gate_node.organization_id=gate.organization_id
        WHERE gate.organization_id=NEW.organization_id AND gate.root_run_id=NEW.id AND gate.project_id=NEW.project_id
            AND gate.id::text=episode->>'gateId' AND gate.ref=episode->>'gateRef' AND gate.state='OPEN'
            AND gate_node.parent_node_id::text=COALESCE(episode->>'managerNodeId',episode->>'finalNodeId')
    ) THEN RAISE EXCEPTION 'workflow recovery gate pin differs' USING ERRCODE='23514'; END IF;
    IF episode->>'state'='RECOVERED' AND NOT EXISTS (
        SELECT 1 FROM control_plane.run_nodes node
        JOIN control_plane.owner_gates gate ON gate.id::text=episode->>'gateId'
            AND gate.organization_id=NEW.organization_id AND gate.root_run_id=NEW.id AND gate.state='APPROVED'
        WHERE node.id::text=episode->>'recoveryNodeId' AND node.organization_id=NEW.organization_id AND node.root_run_id=NEW.id
            AND node.workflow_business_attempt=(episode->>'attempt')::integer+1
            AND COALESCE(node.workflow_step_origin_node_id,node.id)::text=episode->>'originNodeId'
            AND node.workflow_inputs=episode->'inputs' AND (episode->>'attempt')::integer<4
            AND (episode->>'episode')::integer<48
    ) THEN RAISE EXCEPTION 'workflow fresh recovery execution differs' USING ERRCODE='23514'; END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_workflow_exception_episodes() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.guard_workflow_exception_episodes() TO control_plane_runtime;
CREATE TRIGGER workflow_exception_episodes_guard BEFORE INSERT OR UPDATE ON control_plane.runs
    FOR EACH ROW EXECUTE FUNCTION control_plane.guard_workflow_exception_episodes();
RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'workflow exception episodes migration is forward-only'; END $$;
-- +goose StatementEnd
