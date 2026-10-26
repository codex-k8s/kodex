-- +goose Up
SET ROLE control_plane_owner;
SET LOCAL lock_timeout = '8s';
SET LOCAL statement_timeout = '30s';

-- Отдельная owner-линия прежнего workflow; SERVER business/origin не меняются.
ALTER TABLE control_plane.run_nodes
    ADD COLUMN owner_rework_gate_id uuid,
    ADD COLUMN owner_rework_origin_node_id uuid,
    ADD CONSTRAINT legacy_rework_gate_fk FOREIGN KEY (owner_rework_gate_id)
        REFERENCES control_plane.owner_gates(id) DEFERRABLE INITIALLY IMMEDIATE,
    ADD CONSTRAINT legacy_rework_origin_fk FOREIGN KEY (owner_rework_origin_node_id)
        REFERENCES control_plane.run_nodes(id) DEFERRABLE INITIALLY IMMEDIATE,
    ADD CONSTRAINT legacy_rework_origin_requires_gate CHECK (
        owner_rework_origin_node_id IS NULL OR owner_rework_gate_id IS NOT NULL);

-- Допускаем только доказанные owner-attempt вместо второго обычного anchor.
-- +goose StatementBegin
DO $migration$
BEGIN
    IF (SELECT pg_get_expr(index_row.indpred,index_row.indrelid)
        FROM pg_index index_row WHERE index_row.indexrelid='control_plane.run_nodes_workflow_step_once'::regclass)
        IS DISTINCT FROM '((workflow_step_key <> ''''::text) AND (workflow_step_origin_node_id IS NULL))' THEN
        RAISE EXCEPTION 'legacy workflow step index source changed';
    END IF;
END $migration$;
-- +goose StatementEnd
DROP INDEX control_plane.run_nodes_workflow_step_once;
CREATE UNIQUE INDEX run_nodes_workflow_step_once
    ON control_plane.run_nodes(root_run_id,workflow_step_key)
    WHERE workflow_step_key<>'' AND workflow_step_origin_node_id IS NULL AND owner_rework_gate_id IS NULL;
CREATE UNIQUE INDEX run_nodes_owner_rework_once ON control_plane.run_nodes(owner_rework_gate_id)
    WHERE owner_rework_gate_id IS NOT NULL AND owner_rework_origin_node_id IS NULL;
CREATE UNIQUE INDEX run_nodes_owner_rework_execution ON control_plane.run_nodes(owner_rework_origin_node_id,attempt)
    WHERE owner_rework_origin_node_id IS NOT NULL;

-- +goose StatementBegin
CREATE FUNCTION control_plane.legacy_workflow_step_origin(p_organization uuid,p_node uuid) RETURNS SETOF uuid
LANGUAGE sql STABLE SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $$
WITH RECURSIVE selected AS MATERIALIZED (
    SELECT node.*,version.spec,agent.ref AS agent_ref
    FROM control_plane.run_nodes node
    JOIN control_plane.runs root ON root.id=node.root_run_id AND root.organization_id=p_organization AND root.root_run_id=root.id
    JOIN control_plane.runs physical ON physical.id=node.run_id AND physical.organization_id=p_organization
        AND physical.root_run_id=root.id AND physical.project_id=root.project_id
    JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=p_organization
    JOIN control_plane.workflows workflow ON workflow.id=version.workflow_id AND workflow.organization_id=p_organization
        AND workflow.project_id=root.project_id
    JOIN control_plane.agents agent ON agent.id=node.agent_id AND agent.organization_id=p_organization AND agent.project_id=root.project_id
    WHERE node.id=p_node AND node.organization_id=p_organization AND node.type='AGENT_EXECUTION'
        AND node.materialization_state='MATERIALIZED' AND node.workflow_business_attempt=0
        AND COALESCE(version.spec->>'ExecutionMode','')<>'SERVER_DAG_V1'
), budget AS MATERIALIZED (
    SELECT count(*) AS nodes FROM control_plane.run_nodes WHERE organization_id=p_organization
        AND root_run_id=(SELECT root_run_id FROM selected)
), lineage AS (
    SELECT selected.id,selected.attempt,ARRAY[selected.id] AS path,0 AS depth FROM selected
    UNION ALL
    SELECT previous.id,previous.attempt,lineage.path||previous.id,lineage.depth+1
    FROM lineage JOIN selected ON true
    JOIN control_plane.run_edges edge ON edge.target_node_id=lineage.id AND edge.organization_id=p_organization
        AND edge.root_run_id=selected.root_run_id AND edge.type='CONTINUES'
    JOIN control_plane.run_nodes previous ON previous.id=edge.source_node_id AND previous.organization_id=p_organization
        AND previous.root_run_id=selected.root_run_id AND previous.run_id=selected.run_id AND previous.agent_id=selected.agent_id
        AND previous.attempt+1=lineage.attempt AND previous.state='SUCCEEDED'
    CROSS JOIN budget WHERE lineage.depth<budget.nodes AND NOT previous.id=ANY(lineage.path)
), candidates AS (
    SELECT first_node.id FROM lineage JOIN selected ON true
    JOIN control_plane.run_nodes first_node ON first_node.id=lineage.id AND first_node.organization_id=p_organization
    WHERE NOT EXISTS (SELECT 1 FROM control_plane.run_edges edge WHERE edge.target_node_id=first_node.id AND edge.type='CONTINUES')
        AND EXISTS (SELECT 1 FROM jsonb_array_elements(selected.spec->'Steps') step(value)
            WHERE step.value->>'Key'=first_node.workflow_step_key AND step.value->>'AgentRef'=selected.agent_ref)
        AND NOT EXISTS (SELECT 1 FROM control_plane.run_edges branch WHERE branch.target_node_id=ANY(lineage.path)
            AND branch.type='CONTINUES' GROUP BY branch.target_node_id HAVING count(*)>1)
)
SELECT id FROM candidates WHERE (SELECT count(*) FROM candidates)=1;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.legacy_workflow_step_origin(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.legacy_workflow_step_origin(uuid,uuid) TO control_plane_runtime;

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_legacy_workflow_rework() RETURNS trigger
LANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $$
DECLARE original record; first_node record;
BEGIN
    IF TG_OP='DELETE' THEN
        IF OLD.owner_rework_gate_id IS NOT NULL THEN
            IF NOT control_plane.project_purge_authorized() OR NOT EXISTS (
                SELECT 1 FROM control_plane.project_purge_targets target
                JOIN control_plane.project_purge_context context USING (transaction_id)
                WHERE target.transaction_id=pg_current_xact_id_if_assigned() AND context.backend_pid=pg_backend_pid()
                    AND target.relation_id='control_plane.run_nodes'::regclass AND target.row_key=jsonb_build_array(OLD.id)
            ) THEN RAISE EXCEPTION 'legacy rework requires exact authorized project purge' USING ERRCODE='23514'; END IF;
        END IF;
        RETURN OLD;
    END IF;
    IF TG_OP='UPDATE' AND OLD.owner_rework_gate_id IS NOT NULL AND ROW(
        NEW.id,NEW.ref,NEW.organization_id,NEW.root_run_id,NEW.run_id,NEW.parent_node_id,NEW.agent_id,
        NEW.turn_id,NEW.workflow_step_key,NEW.attempt,NEW.owner_rework_gate_id,NEW.owner_rework_origin_node_id,NEW.human_gate_after
    ) IS DISTINCT FROM ROW(
        OLD.id,OLD.ref,OLD.organization_id,OLD.root_run_id,OLD.run_id,OLD.parent_node_id,OLD.agent_id,
        OLD.turn_id,OLD.workflow_step_key,OLD.attempt,OLD.owner_rework_gate_id,OLD.owner_rework_origin_node_id,OLD.human_gate_after
    ) THEN RAISE EXCEPTION 'legacy rework assignment is immutable' USING ERRCODE='23514'; END IF;
    IF TG_OP='UPDATE' AND OLD.owner_rework_gate_id IS NULL AND NEW.owner_rework_gate_id IS NOT NULL THEN
        RAISE EXCEPTION 'legacy rework cannot reinterpret an existing node' USING ERRCODE='23514';
    END IF;
    IF NEW.owner_rework_gate_id IS NULL THEN RETURN NEW; END IF;
    IF NEW.workflow_business_attempt<>0 OR NEW.workflow_step_origin_node_id IS NOT NULL
       OR NEW.type<>'AGENT_EXECUTION' OR NEW.materialization_state<>'MATERIALIZED' OR NOT NEW.human_gate_after THEN
        RAISE EXCEPTION 'legacy rework cannot impersonate SERVER provenance' USING ERRCODE='23514';
    END IF;
    SELECT predecessor.id AS node_id,predecessor.run_id,logical_origin.workflow_step_key,
           predecessor.agent_id,prior.attempt AS run_attempt,prior.project_id,prior.initiated_by,
           root.id AS root_id,root.workflow_version_id
    INTO original
    FROM control_plane.owner_gates gate
    JOIN control_plane.run_nodes gate_node ON gate_node.id=gate.node_id AND gate_node.organization_id=gate.organization_id
        AND gate_node.root_run_id=gate.root_run_id AND gate_node.type='HUMAN_GATE'
    JOIN control_plane.run_nodes predecessor ON predecessor.id=gate_node.parent_node_id
        AND predecessor.organization_id=gate.organization_id AND predecessor.root_run_id=gate.root_run_id
    JOIN LATERAL control_plane.legacy_workflow_step_origin(NEW.organization_id,predecessor.id) origin_id ON true
    JOIN control_plane.run_nodes logical_origin ON logical_origin.id=origin_id AND logical_origin.organization_id=NEW.organization_id
    JOIN control_plane.runs prior ON prior.id=predecessor.run_id AND prior.organization_id=gate.organization_id
    JOIN control_plane.runs root ON root.id=gate.root_run_id AND root.organization_id=gate.organization_id
        AND root.project_id=gate.project_id AND root.project_id=prior.project_id AND root.id<>prior.id
    JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
    JOIN control_plane.workflows workflow ON workflow.id=version.workflow_id AND workflow.organization_id=root.organization_id
        AND workflow.project_id=root.project_id
    JOIN control_plane.agents agent ON agent.id=predecessor.agent_id AND agent.organization_id=root.organization_id
        AND agent.project_id=root.project_id
    WHERE gate.id=NEW.owner_rework_gate_id AND gate.organization_id=NEW.organization_id AND gate.root_run_id=NEW.root_run_id
        AND gate.state='CHANGES_REQUESTED' AND gate.decision='REQUEST_CHANGES'
        AND gate.allowed_decisions @> ARRAY['REQUEST_CHANGES']::text[] AND gate.integration_invocation_id IS NULL
        AND gate.scope_kind='PROJECT' AND COALESCE(version.spec->>'ExecutionMode','')<>'SERVER_DAG_V1'
        AND predecessor.workflow_business_attempt=0 AND predecessor.workflow_step_origin_node_id IS NULL
        AND predecessor.state='SUCCEEDED' AND prior.state='SUCCEEDED'
        AND EXISTS (SELECT 1 FROM jsonb_array_elements(version.spec->'Steps') step(value)
            WHERE step.value->>'Key'=logical_origin.workflow_step_key AND step.value->>'AgentRef'=agent.ref
                AND step.value->>'HumanGateAfter'='true');
    IF NOT FOUND OR NEW.agent_id<>original.agent_id OR NEW.workflow_step_key<>original.workflow_step_key
        OR NOT EXISTS (SELECT 1 FROM control_plane.runs physical
            JOIN control_plane.session_turns turn ON turn.id=NEW.turn_id AND turn.organization_id=NEW.organization_id
                AND turn.run_id=physical.id AND turn.session_id=physical.session_id
            WHERE physical.id=NEW.run_id AND physical.organization_id=NEW.organization_id
                AND physical.root_run_id=original.root_id AND physical.project_id=original.project_id
                AND physical.workflow_version_id=original.workflow_version_id
                AND physical.retry_of_run_id=original.run_id AND physical.attempt=original.run_attempt+1
                AND physical.source='AGENT_DELEGATION' AND physical.initiated_by=original.initiated_by)
    THEN RAISE EXCEPTION 'legacy rework does not match its stored owner decision' USING ERRCODE='23514'; END IF;
    IF NEW.owner_rework_origin_node_id IS NOT NULL THEN
        SELECT * INTO first_node FROM control_plane.run_nodes origin WHERE origin.id=NEW.owner_rework_origin_node_id;
        IF NOT FOUND OR first_node.id=NEW.id OR first_node.owner_rework_origin_node_id IS NOT NULL
            OR first_node.owner_rework_gate_id IS DISTINCT FROM NEW.owner_rework_gate_id
            OR ROW(first_node.organization_id,first_node.root_run_id,first_node.run_id,first_node.agent_id,first_node.workflow_step_key)
                IS DISTINCT FROM ROW(NEW.organization_id,NEW.root_run_id,NEW.run_id,NEW.agent_id,NEW.workflow_step_key)
            OR NEW.attempt<=first_node.attempt THEN
            RAISE EXCEPTION 'legacy technical continuation lost its owner origin' USING ERRCODE='23514';
        END IF;
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER legacy_workflow_rework_guard BEFORE INSERT OR UPDATE OR DELETE ON control_plane.run_nodes
    FOR EACH ROW EXECUTE FUNCTION control_plane.guard_legacy_workflow_rework();
REVOKE ALL ON FUNCTION control_plane.guard_legacy_workflow_rework() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.guard_legacy_workflow_rework() TO control_plane_runtime;

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_legacy_workflow_rework_edges() RETURNS trigger
LANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $$
BEGIN
    IF NEW.owner_rework_gate_id IS NULL THEN RETURN NULL; END IF;
    IF NOT EXISTS (SELECT 1 FROM control_plane.run_nodes current_node WHERE current_node.id=NEW.id) THEN
        IF control_plane.project_purge_authorized() AND EXISTS (
            SELECT 1 FROM control_plane.project_purge_targets target
            JOIN control_plane.project_purge_context context USING (transaction_id)
            WHERE target.transaction_id=pg_current_xact_id_if_assigned() AND context.backend_pid=pg_backend_pid()
                AND target.relation_id='control_plane.run_nodes'::regclass AND target.row_key=jsonb_build_array(NEW.id)
        ) THEN RETURN NULL; END IF;
        RAISE EXCEPTION 'legacy rework deferred target disappeared' USING ERRCODE='23514';
    END IF;
    IF NEW.owner_rework_origin_node_id IS NULL THEN
        IF NOT EXISTS (
            WITH RECURSIVE budget AS MATERIALIZED (
                SELECT count(*) AS nodes FROM control_plane.run_nodes
                WHERE organization_id=NEW.organization_id AND root_run_id=NEW.root_run_id
            ), lineage AS (
                SELECT original_parent.id,original_parent.run_id,original_parent.agent_id,ARRAY[original_parent.id] AS path,0 AS depth
                FROM control_plane.owner_gates gate
                JOIN control_plane.run_nodes gate_node ON gate_node.id=gate.node_id AND gate_node.organization_id=NEW.organization_id
                    AND gate_node.root_run_id=NEW.root_run_id
                JOIN LATERAL control_plane.legacy_workflow_step_origin(NEW.organization_id,gate_node.parent_node_id) origin_id ON true
                JOIN control_plane.run_edges callback ON callback.organization_id=NEW.organization_id
                    AND callback.root_run_id=NEW.root_run_id AND callback.source_node_id=origin_id
                    AND callback.type='CALLBACK_TO'
                JOIN control_plane.run_nodes original_parent ON original_parent.id=callback.target_node_id
                    AND original_parent.organization_id=NEW.organization_id AND original_parent.root_run_id=NEW.root_run_id
                WHERE gate.id=NEW.owner_rework_gate_id AND gate.organization_id=NEW.organization_id
                UNION ALL
                SELECT next_node.id,next_node.run_id,next_node.agent_id,lineage.path||next_node.id,lineage.depth+1
                FROM lineage JOIN control_plane.run_edges continuation ON continuation.source_node_id=lineage.id
                    AND continuation.organization_id=NEW.organization_id AND continuation.root_run_id=NEW.root_run_id
                    AND continuation.type='CONTINUES'
                JOIN control_plane.run_nodes next_node ON next_node.id=continuation.target_node_id
                    AND next_node.organization_id=NEW.organization_id AND next_node.root_run_id=NEW.root_run_id
                    AND next_node.run_id=lineage.run_id AND next_node.agent_id=lineage.agent_id
                CROSS JOIN budget WHERE lineage.depth<budget.nodes AND NOT next_node.id=ANY(lineage.path)
            ) SELECT 1 FROM lineage WHERE lineage.id=NEW.parent_node_id
                AND NOT EXISTS (SELECT 1 FROM control_plane.run_edges branch
                    WHERE branch.source_node_id=ANY(lineage.path) AND branch.type='CONTINUES'
                    GROUP BY branch.source_node_id HAVING count(*)>1)
                AND (TG_OP<>'INSERT' OR NOT EXISTS (SELECT 1 FROM control_plane.run_edges future
                    WHERE future.source_node_id=lineage.id AND future.type='CONTINUES'))
        ) THEN RAISE EXCEPTION 'legacy rework parent is outside exact original callback lineage' USING ERRCODE='23514'; END IF;
        IF NOT EXISTS (
            SELECT 1 FROM control_plane.run_edges callback
            JOIN control_plane.run_edges delegation ON delegation.organization_id=NEW.organization_id
                AND delegation.root_run_id=NEW.root_run_id AND delegation.source_node_id=callback.target_node_id
                AND delegation.target_node_id=NEW.id AND delegation.type='DELEGATED_TO'
            JOIN control_plane.run_nodes parent ON parent.id=callback.target_node_id
                AND parent.organization_id=NEW.organization_id AND parent.root_run_id=NEW.root_run_id
            JOIN control_plane.runs root ON root.id=NEW.root_run_id AND root.organization_id=NEW.organization_id
                AND parent.run_id=root.id
            JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
            JOIN control_plane.agents agent ON agent.id=parent.agent_id AND agent.organization_id=root.organization_id
                AND agent.ref=version.spec->>'CoordinatorAgentRef'
            WHERE callback.organization_id=NEW.organization_id AND callback.root_run_id=NEW.root_run_id
                AND callback.source_node_id=NEW.id AND callback.type='CALLBACK_TO'
                AND NEW.parent_node_id=parent.id AND parent.state IN ('SUCCEEDED','CANCELLED')
                AND NOT EXISTS (SELECT 1 FROM control_plane.runtime_leases lease
                    WHERE lease.node_id=parent.id AND lease.state='CLAIMED')
                AND (parent.state='SUCCEEDED' OR NOT EXISTS (SELECT 1 FROM control_plane.runtime_revisions revision WHERE revision.node_id=parent.id))
        ) THEN RAISE EXCEPTION 'legacy rework lacks its sealed callback boundary' USING ERRCODE='23514'; END IF;
    ELSIF NOT EXISTS (
        SELECT 1 FROM control_plane.run_edges edge
        JOIN control_plane.run_nodes previous ON previous.id=edge.source_node_id AND previous.organization_id=NEW.organization_id
            AND previous.root_run_id=NEW.root_run_id AND previous.run_id=NEW.run_id AND previous.agent_id=NEW.agent_id
            AND previous.owner_rework_gate_id=NEW.owner_rework_gate_id AND previous.state='SUCCEEDED'
            AND COALESCE(previous.owner_rework_origin_node_id,previous.id)=NEW.owner_rework_origin_node_id
            AND previous.attempt+1=NEW.attempt AND previous.workflow_step_key=NEW.workflow_step_key
        WHERE edge.organization_id=NEW.organization_id AND edge.root_run_id=NEW.root_run_id
            AND edge.target_node_id=NEW.id AND edge.type='CONTINUES' AND NEW.parent_node_id=previous.id
    ) THEN RAISE EXCEPTION 'legacy rework technical edge is not exact' USING ERRCODE='23514'; END IF;
    RETURN NULL;
END $$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER legacy_workflow_rework_edges_guard AFTER INSERT OR UPDATE ON control_plane.run_nodes
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION control_plane.guard_legacy_workflow_rework_edges();
REVOKE ALL ON FUNCTION control_plane.guard_legacy_workflow_rework_edges() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.guard_legacy_workflow_rework_edges() TO control_plane_runtime;

-- Новый graph допускается только как точная композиция прежних284 FK и двух
-- названных deferred NO ACTION связей. Тело purge и его ACL сохраняются.
-- +goose StatementBegin
DO $migration$
DECLARE definition text; old_edges text; all_nodes oid[]; nodes_count integer; nodes_hash text; edges_count integer; edges_hash text;
BEGIN
    SELECT pg_get_functiondef('control_plane.purge_project_database(uuid,uuid,text)'::regprocedure) INTO definition;
    old_edges:=substring(definition FROM 'IF v_edges <> [0-9]+ OR v_edge_fingerprint <> ''[a-f0-9]{32}'' THEN');
    IF old_edges IS DISTINCT FROM 'IF v_edges <> 284 OR v_edge_fingerprint <> ''6bf6ad76d8c7ae66e07f75ecf719fdd6'' THEN'
       OR substring(definition FROM 'IF v_nodes <> [0-9]+ OR v_node_fingerprint <> ''[a-f0-9]{32}'' THEN')
          IS DISTINCT FROM 'IF v_nodes <> 105 OR v_node_fingerprint <> ''f51a0de25bd942a250997775617eeaee'' THEN' THEN
        RAISE EXCEPTION 'legacy rework purge preimage changed';
    END IF;
    WITH RECURSIVE nodes(relation_id) AS (
        SELECT 'control_plane.projects'::regclass::oid UNION
        SELECT fk.conrelid FROM nodes JOIN pg_constraint fk ON fk.confrelid=nodes.relation_id
            AND fk.contype='f' AND fk.connamespace='control_plane'::regnamespace
    ) SELECT array_agg(relation_id) INTO all_nodes FROM nodes;
    SELECT count(*),md5(string_agg(namespace.nspname||'.'||relation.relname,E'\n' ORDER BY namespace.nspname,relation.relname))
        INTO nodes_count,nodes_hash FROM pg_class relation JOIN pg_namespace namespace ON namespace.oid=relation.relnamespace
        WHERE relation.oid=ANY(all_nodes);
    SELECT count(*),md5(string_agg(child_ns.nspname||'.'||child.relname||'|'||fk.conname||'|'
        ||parent_ns.nspname||'.'||parent.relname||'|'||fk.confdeltype::text,E'\n' ORDER BY child_ns.nspname,child.relname,fk.conname))
        INTO edges_count,edges_hash FROM pg_constraint fk JOIN pg_class child ON child.oid=fk.conrelid
        JOIN pg_namespace child_ns ON child_ns.oid=child.relnamespace JOIN pg_class parent ON parent.oid=fk.confrelid
        JOIN pg_namespace parent_ns ON parent_ns.oid=parent.relnamespace
        WHERE fk.contype='f' AND fk.confrelid=ANY(all_nodes) AND fk.conrelid=ANY(all_nodes)
            AND NOT (fk.conrelid='control_plane.run_nodes'::regclass AND fk.conname IN ('legacy_rework_gate_fk','legacy_rework_origin_fk'));
    IF nodes_count<>105 OR nodes_hash<>'f51a0de25bd942a250997775617eeaee' OR edges_count<>284
       OR edges_hash<>'6bf6ad76d8c7ae66e07f75ecf719fdd6'
       OR (SELECT count(*) FROM pg_constraint fk WHERE fk.conrelid='control_plane.run_nodes'::regclass
            AND fk.conname IN ('legacy_rework_gate_fk','legacy_rework_origin_fk') AND fk.contype='f'
            AND fk.condeferrable AND NOT fk.condeferred AND fk.confdeltype='a')<>2 THEN
        RAISE EXCEPTION 'legacy rework purge graph composition changed';
    END IF;
    SELECT count(*),md5(string_agg(child_ns.nspname||'.'||child.relname||'|'||fk.conname||'|'
        ||parent_ns.nspname||'.'||parent.relname||'|'||fk.confdeltype::text,E'\n' ORDER BY child_ns.nspname,child.relname,fk.conname))
        INTO edges_count,edges_hash FROM pg_constraint fk JOIN pg_class child ON child.oid=fk.conrelid
        JOIN pg_namespace child_ns ON child_ns.oid=child.relnamespace JOIN pg_class parent ON parent.oid=fk.confrelid
        JOIN pg_namespace parent_ns ON parent_ns.oid=parent.relnamespace
        WHERE fk.contype='f' AND fk.confrelid=ANY(all_nodes) AND fk.conrelid=ANY(all_nodes);
    IF edges_count<>286 THEN RAISE EXCEPTION 'legacy rework purge edge count changed'; END IF;
    EXECUTE replace(definition,old_edges,format('IF v_edges <> %s OR v_edge_fingerprint <> %L THEN',edges_count,edges_hash));
END $migration$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION control_plane.legacy_rework_callback_origin(p_organization uuid,p_child uuid,p_revision uuid)
RETURNS TABLE(eligible boolean,original_node_id uuid,final_node_id uuid,callback_edge_id uuid,callback_edge_ref text,
    parent_node_id uuid,parent_node_ref text,parent_run_id uuid,revision_ref text,runtime_attempt integer,generation bigint)
LANGUAGE sql STABLE SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $$
WITH RECURSIVE final AS MATERIALIZED (
    SELECT node.*,revision.ref AS revision_ref,revision.attempt AS revision_attempt,revision.generation AS revision_generation,
        root.project_id,root.initiated_by,COALESCE(node.owner_rework_origin_node_id,node.id) AS first_id
    FROM control_plane.runtime_revisions revision
    JOIN control_plane.run_nodes node ON node.id=revision.node_id AND node.organization_id=p_organization
        AND node.run_id=p_child AND node.state='SUCCEEDED' AND node.owner_rework_gate_id IS NOT NULL
        AND node.agent_id=revision.agent_id AND node.turn_id=revision.turn_id AND node.attempt=revision.attempt
    JOIN control_plane.runs child ON child.id=node.run_id AND child.organization_id=p_organization
        AND child.session_id=revision.session_id AND child.state='SUCCEEDED'
    JOIN control_plane.runs root ON root.id=node.root_run_id AND root.organization_id=p_organization
        AND root.id=child.root_run_id AND root.project_id=child.project_id AND revision.project_id=root.project_id
        AND revision.root_run_id=root.id AND root.state IN ('RUNNING','WAITING_HUMAN')
    JOIN control_plane.runtime_leases lease ON lease.runtime_revision_id=revision.id AND lease.organization_id=p_organization
        AND lease.run_id=p_child AND lease.node_id=node.id AND lease.generation=revision.generation AND lease.state='COMPLETED'
    WHERE revision.id=p_revision AND revision.organization_id=p_organization AND revision.run_id=p_child
), budget AS MATERIALIZED (
    SELECT count(*) AS nodes FROM control_plane.run_nodes WHERE organization_id=p_organization
        AND root_run_id=(SELECT root_run_id FROM final)
), lineage AS (
    SELECT origin.id,origin.attempt,ARRAY[origin.id] AS path,0 AS depth
    FROM final JOIN control_plane.run_nodes origin ON origin.id=final.first_id
        AND origin.organization_id=p_organization AND origin.root_run_id=final.root_run_id AND origin.run_id=p_child
        AND origin.agent_id=final.agent_id AND origin.workflow_step_key=final.workflow_step_key
        AND origin.owner_rework_gate_id=final.owner_rework_gate_id AND origin.owner_rework_origin_node_id IS NULL
        AND origin.state='SUCCEEDED'
    UNION ALL
    SELECT next_node.id,next_node.attempt,lineage.path||next_node.id,lineage.depth+1
    FROM lineage JOIN final ON true
    JOIN control_plane.run_edges edge ON edge.source_node_id=lineage.id AND edge.organization_id=p_organization
        AND edge.root_run_id=final.root_run_id AND edge.type='CONTINUES'
    JOIN control_plane.run_nodes next_node ON next_node.id=edge.target_node_id AND next_node.organization_id=p_organization
        AND next_node.root_run_id=final.root_run_id AND next_node.run_id=p_child AND next_node.agent_id=final.agent_id
        AND next_node.owner_rework_gate_id=final.owner_rework_gate_id AND next_node.owner_rework_origin_node_id=final.first_id
        AND next_node.workflow_step_key=final.workflow_step_key AND next_node.attempt=lineage.attempt+1 AND next_node.state='SUCCEEDED'
    CROSS JOIN budget WHERE lineage.depth<budget.nodes AND NOT next_node.id=ANY(lineage.path)
)
SELECT true,origin.id,final.id,callback.id,callback.ref,parent.id,parent.ref,parent.run_id,
    final.revision_ref,final.revision_attempt,final.revision_generation
FROM final JOIN lineage ON lineage.id=final.id
JOIN control_plane.run_nodes origin ON origin.id=final.first_id AND origin.organization_id=p_organization
JOIN control_plane.owner_gates owner_decision ON owner_decision.id=final.owner_rework_gate_id AND owner_decision.organization_id=p_organization
    AND owner_decision.root_run_id=final.root_run_id AND owner_decision.project_id=final.project_id
    AND owner_decision.state='CHANGES_REQUESTED' AND owner_decision.decision='REQUEST_CHANGES'
JOIN control_plane.run_edges callback ON callback.organization_id=p_organization AND callback.root_run_id=final.root_run_id
    AND callback.source_node_id=origin.id AND callback.type='CALLBACK_TO'
JOIN control_plane.run_nodes parent ON parent.id=callback.target_node_id AND parent.organization_id=p_organization
    AND parent.root_run_id=final.root_run_id AND parent.id=origin.parent_node_id
JOIN control_plane.runs parent_run ON parent_run.id=parent.run_id AND parent_run.organization_id=p_organization
    AND parent_run.project_id=final.project_id AND parent_run.initiated_by=final.initiated_by
JOIN control_plane.run_edges delegation ON delegation.organization_id=p_organization AND delegation.root_run_id=final.root_run_id
    AND delegation.source_node_id=parent.id AND delegation.target_node_id=origin.id AND delegation.type='DELEGATED_TO'
WHERE NOT EXISTS (SELECT 1 FROM control_plane.run_edges future WHERE future.source_node_id=final.id AND future.type='CONTINUES')
    AND NOT EXISTS (SELECT 1 FROM control_plane.run_edges branch WHERE branch.source_node_id=ANY(lineage.path)
        AND branch.type='CONTINUES' GROUP BY branch.source_node_id HAVING count(*)>1)
    AND (SELECT count(*) FROM lineage WHERE id=final.id)=1;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.legacy_rework_callback_origin(uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.legacy_rework_callback_origin(uuid,uuid,uuid) TO control_plane_runtime;

-- +goose StatementBegin
CREATE FUNCTION control_plane.legacy_rework_execution_assigned(p_organization uuid,p_node uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $$
SELECT EXISTS (
    SELECT 1 FROM control_plane.run_nodes node
    JOIN control_plane.run_nodes first_node ON first_node.id=COALESCE(node.owner_rework_origin_node_id,node.id)
        AND first_node.organization_id=p_organization AND first_node.run_id=node.run_id
        AND first_node.root_run_id=node.root_run_id AND first_node.agent_id=node.agent_id
        AND first_node.workflow_step_key=node.workflow_step_key
        AND first_node.owner_rework_gate_id=node.owner_rework_gate_id AND first_node.owner_rework_origin_node_id IS NULL
    JOIN control_plane.runs physical ON physical.id=node.run_id AND physical.organization_id=p_organization
        AND physical.state IN ('QUEUED','RUNNING')
    JOIN control_plane.runs root ON root.id=node.root_run_id AND root.organization_id=p_organization
        AND root.root_run_id=root.id AND root.id=physical.root_run_id AND root.project_id=physical.project_id
        AND root.workflow_version_id=physical.workflow_version_id AND root.state IN ('QUEUED','RUNNING','WAITING_HUMAN')
    JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=p_organization
    JOIN control_plane.workflows workflow ON workflow.id=version.workflow_id AND workflow.organization_id=p_organization
        AND workflow.project_id=root.project_id
    JOIN control_plane.agents agent ON agent.id=node.agent_id AND agent.organization_id=p_organization AND agent.project_id=root.project_id
    JOIN control_plane.owner_gates decision ON decision.id=node.owner_rework_gate_id AND decision.organization_id=p_organization
        AND decision.root_run_id=root.id AND decision.project_id=root.project_id
        AND decision.state='CHANGES_REQUESTED' AND decision.decision='REQUEST_CHANGES'
    JOIN control_plane.run_nodes gate_node ON gate_node.id=decision.node_id AND gate_node.organization_id=p_organization
        AND gate_node.root_run_id=root.id
    JOIN control_plane.run_nodes predecessor ON predecessor.id=gate_node.parent_node_id AND predecessor.organization_id=p_organization
        AND predecessor.root_run_id=root.id AND predecessor.run_id=physical.retry_of_run_id
        AND predecessor.agent_id=node.agent_id
    JOIN LATERAL control_plane.legacy_workflow_step_origin(p_organization,predecessor.id) origin_id ON true
    JOIN control_plane.run_nodes logical_origin ON logical_origin.id=origin_id AND logical_origin.workflow_step_key=node.workflow_step_key
    WHERE node.id=p_node AND node.organization_id=p_organization AND node.owner_rework_gate_id IS NOT NULL
        AND node.type='AGENT_EXECUTION' AND node.materialization_state='MATERIALIZED'
        AND node.workflow_business_attempt=0 AND node.workflow_step_origin_node_id IS NULL
        AND COALESCE(version.spec->>'ExecutionMode','')<>'SERVER_DAG_V1'
        AND EXISTS (SELECT 1 FROM jsonb_array_elements(version.spec->'Steps') step(value)
            WHERE step.value->>'Key'=node.workflow_step_key AND step.value->>'AgentRef'=agent.ref)
);
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.legacy_rework_execution_assigned(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.legacy_rework_execution_assigned(uuid,uuid) TO control_plane_runtime;

-- +goose StatementBegin
CREATE FUNCTION control_plane.legacy_rework_delivery_pending(p_organization uuid,p_root uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $$
SELECT EXISTS (
    SELECT 1 FROM control_plane.runs root
    JOIN control_plane.run_nodes rework ON rework.organization_id=p_organization AND rework.root_run_id=root.id
        AND rework.owner_rework_gate_id IS NOT NULL AND rework.owner_rework_origin_node_id IS NULL
    JOIN control_plane.run_edges callback ON callback.organization_id=p_organization AND callback.root_run_id=root.id
        AND callback.source_node_id=rework.id AND callback.type='CALLBACK_TO' AND callback.target_node_id=rework.parent_node_id
    WHERE root.id=p_root AND root.organization_id=p_organization AND root.root_run_id=root.id
        AND root.state IN ('QUEUED','RUNNING','WAITING_HUMAN')
        AND NOT EXISTS (SELECT 1 FROM control_plane.run_edges continuation
            WHERE continuation.organization_id=p_organization AND continuation.root_run_id=root.id
                AND continuation.source_node_id=callback.target_node_id AND continuation.type='CONTINUES')
);
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.legacy_rework_delivery_pending(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.legacy_rework_delivery_pending(uuid,uuid) TO control_plane_runtime;
RESET ROLE;

-- +goose Down
SELECT 1;
