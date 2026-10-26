-- +goose Up
SET ROLE control_plane_owner;
SET LOCAL lock_timeout = '8s';
SET LOCAL statement_timeout = '30s';

-- Logical anchor сохраняет историю, physical node принадлежит одной попытке.
-- Старые materialized rows остаются с business_attempt=0 и не переинтерпретируются.
ALTER TABLE control_plane.run_nodes
    ADD COLUMN workflow_step_origin_node_id uuid,
    ADD COLUMN workflow_business_attempt integer NOT NULL DEFAULT 0,
    ADD COLUMN workflow_captured_artifacts jsonb,
    ADD COLUMN workflow_attempts jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN workflow_inputs jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN workflow_effect_intents jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN workflow_effect_rejoins jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD CONSTRAINT workflow_physical_origin_fk FOREIGN KEY (workflow_step_origin_node_id)
        REFERENCES control_plane.run_nodes(id) DEFERRABLE INITIALLY IMMEDIATE,
    ADD CONSTRAINT workflow_business_attempt_bound CHECK (workflow_business_attempt BETWEEN 0 AND 4),
    ADD CONSTRAINT workflow_capture_bound CHECK (
        workflow_captured_artifacts IS NULL OR
        (jsonb_typeof(workflow_captured_artifacts)='object' AND octet_length(workflow_captured_artifacts::text)<=32768)),
    ADD CONSTRAINT workflow_journal_bounds CHECK (
        jsonb_typeof(workflow_attempts)='array' AND jsonb_array_length(workflow_attempts)<=4
        AND jsonb_typeof(workflow_inputs)='array' AND jsonb_array_length(workflow_inputs)<=8
        AND jsonb_typeof(workflow_effect_intents)='array' AND jsonb_array_length(workflow_effect_intents)<=1000
        AND jsonb_typeof(workflow_effect_rejoins)='array' AND jsonb_array_length(workflow_effect_rejoins)<=1000);
ALTER TABLE control_plane.runs ADD COLUMN workflow_exception jsonb;

DROP INDEX control_plane.run_nodes_workflow_step_once;
CREATE UNIQUE INDEX run_nodes_workflow_step_once
    ON control_plane.run_nodes(root_run_id,workflow_step_key)
    WHERE workflow_step_key<>'' AND workflow_step_origin_node_id IS NULL;
CREATE INDEX run_nodes_workflow_physical_attempt
    ON control_plane.run_nodes(workflow_step_origin_node_id,workflow_business_attempt,attempt)
    WHERE workflow_step_origin_node_id IS NOT NULL;

-- Новый self-FK меняет граф удаления проекта. Подтверждаем прежний граф без
-- единственного добавленного ребра, затем вычисляем новый fingerprint из каталога.
-- Ни одно постороннее изменение схемы не получает автоматический допуск.
-- +goose StatementBegin
DO $migration$
DECLARE definition text; old_edges text; nodes_count integer; nodes_hash text;
        edges_count integer; edges_hash text; all_nodes oid[];
BEGIN
    SELECT pg_get_functiondef('control_plane.purge_project_database(uuid,uuid,text)'::regprocedure) INTO definition;
    old_edges:=substring(definition FROM 'IF v_edges <> [0-9]+ OR v_edge_fingerprint <> ''[a-f0-9]{32}'' THEN');
    IF substring(definition FROM 'IF v_nodes <> [0-9]+ OR v_node_fingerprint <> ''[a-f0-9]{32}'' THEN')
           IS DISTINCT FROM 'IF v_nodes <> 105 OR v_node_fingerprint <> ''f51a0de25bd942a250997775617eeaee'' THEN'
       OR old_edges IS DISTINCT FROM 'IF v_edges <> 283 OR v_edge_fingerprint <> ''be6e23d9e894687a69a19e9b5777ab09'' THEN' THEN
        RAISE EXCEPTION 'workflow purge graph source changed';
    END IF;
    WITH RECURSIVE nodes(relation_id) AS (
        SELECT 'control_plane.projects'::regclass::oid UNION
        SELECT constraint_row.conrelid FROM nodes
        JOIN pg_constraint constraint_row ON constraint_row.confrelid=nodes.relation_id
            AND constraint_row.contype='f' AND constraint_row.connamespace='control_plane'::regnamespace
    ) SELECT array_agg(relation_id) INTO all_nodes FROM nodes;
    SELECT count(*),md5(string_agg(namespace.nspname||'.'||relation.relname,E'\n' ORDER BY namespace.nspname,relation.relname))
        INTO nodes_count,nodes_hash FROM pg_class relation
        JOIN pg_namespace namespace ON namespace.oid=relation.relnamespace WHERE relation.oid=ANY(all_nodes);
    SELECT count(*),md5(string_agg(child_namespace.nspname||'.'||child.relname||'|'||constraint_row.conname||'|'
        ||parent_namespace.nspname||'.'||parent.relname||'|'||constraint_row.confdeltype::text,
        E'\n' ORDER BY child_namespace.nspname,child.relname,constraint_row.conname)) INTO edges_count,edges_hash
        FROM pg_constraint constraint_row JOIN pg_class child ON child.oid=constraint_row.conrelid
        JOIN pg_namespace child_namespace ON child_namespace.oid=child.relnamespace
        JOIN pg_class parent ON parent.oid=constraint_row.confrelid
        JOIN pg_namespace parent_namespace ON parent_namespace.oid=parent.relnamespace
        WHERE constraint_row.contype='f' AND constraint_row.confrelid=ANY(all_nodes) AND constraint_row.conrelid=ANY(all_nodes)
            AND NOT (constraint_row.conrelid='control_plane.run_nodes'::regclass AND constraint_row.conname='workflow_physical_origin_fk');
    IF nodes_count<>105 OR nodes_hash<>'f51a0de25bd942a250997775617eeaee'
       OR edges_count<>283 OR edges_hash<>'be6e23d9e894687a69a19e9b5777ab09' THEN
        RAISE EXCEPTION 'workflow purge baseline graph changed';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='control_plane.run_nodes'::regclass
        AND conname='workflow_physical_origin_fk' AND confrelid=conrelid AND contype='f'
        AND condeferrable AND NOT condeferred AND confdeltype='a') THEN
        RAISE EXCEPTION 'workflow purge origin edge changed';
    END IF;
    SELECT count(*),md5(string_agg(child_namespace.nspname||'.'||child.relname||'|'||constraint_row.conname||'|'
        ||parent_namespace.nspname||'.'||parent.relname||'|'||constraint_row.confdeltype::text,
        E'\n' ORDER BY child_namespace.nspname,child.relname,constraint_row.conname)) INTO edges_count,edges_hash
        FROM pg_constraint constraint_row JOIN pg_class child ON child.oid=constraint_row.conrelid
        JOIN pg_namespace child_namespace ON child_namespace.oid=child.relnamespace
        JOIN pg_class parent ON parent.oid=constraint_row.confrelid
        JOIN pg_namespace parent_namespace ON parent_namespace.oid=parent.relnamespace
        WHERE constraint_row.contype='f' AND constraint_row.confrelid=ANY(all_nodes) AND constraint_row.conrelid=ANY(all_nodes);
    IF edges_count<>284 THEN RAISE EXCEPTION 'workflow purge edge count changed'; END IF;
    EXECUTE replace(definition,old_edges,format('IF v_edges <> %s OR v_edge_fingerprint <> %L THEN',edges_count,edges_hash));
END $migration$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_workflow_physical_lineage() RETURNS trigger
LANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $$
DECLARE origin record;
BEGIN
    IF TG_OP='DELETE' THEN
        IF OLD.workflow_business_attempt<>0 OR OLD.workflow_captured_artifacts IS NOT NULL
           OR OLD.workflow_attempts<>'[]'::jsonb OR OLD.workflow_inputs<>'[]'::jsonb
           OR OLD.workflow_effect_intents<>'[]'::jsonb OR OLD.workflow_effect_rejoins<>'[]'::jsonb THEN
            IF NOT control_plane.project_purge_authorized() THEN
                RAISE EXCEPTION 'workflow provenance requires authorized project purge' USING ERRCODE='23514';
            END IF;
            -- runs могут быть уже удалены в этой транзакции. Защищённый список
            -- targets проверен purge-владельцем и живёт до полного удаления графа.
            IF NOT EXISTS (
                SELECT 1 FROM control_plane.project_purge_targets target
                JOIN control_plane.project_purge_context context USING (transaction_id)
                WHERE target.transaction_id=pg_current_xact_id_if_assigned() AND context.backend_pid=pg_backend_pid()
                    AND target.relation_id='control_plane.run_nodes'::regclass AND target.row_key=jsonb_build_array(OLD.id)
            ) THEN
                RAISE EXCEPTION 'workflow provenance is outside the authorized purge target' USING ERRCODE='23514';
            END IF;
        END IF;
        RETURN OLD;
    END IF;
    IF TG_OP='UPDATE' AND OLD.materialization_state='MATERIALIZED' AND NEW.materialization_state='PLANNED' THEN
        RAISE EXCEPTION 'materialized execution cannot become a planned workflow anchor' USING ERRCODE='23514';
    END IF;
    IF TG_OP='UPDATE' AND OLD.workflow_business_attempt>0 THEN
        IF ROW(NEW.id,NEW.ref,NEW.organization_id,NEW.root_run_id,NEW.run_id,NEW.turn_id,NEW.agent_id,
               NEW.workflow_step_key,NEW.workflow_step_origin_node_id,NEW.workflow_business_attempt,NEW.attempt)
           IS DISTINCT FROM
           ROW(OLD.id,OLD.ref,OLD.organization_id,OLD.root_run_id,OLD.run_id,OLD.turn_id,OLD.agent_id,
               OLD.workflow_step_key,OLD.workflow_step_origin_node_id,OLD.workflow_business_attempt,OLD.attempt) THEN
            RAISE EXCEPTION 'workflow physical execution identity is immutable' USING ERRCODE='23514';
        END IF;
    END IF;
    IF TG_OP='UPDATE' AND OLD.workflow_business_attempt=0 AND NEW.workflow_business_attempt<>0 THEN
        IF OLD.materialization_state<>'PLANNED' OR OLD.state<>'PLANNED'
           OR OLD.turn_id IS NOT NULL OR NEW.materialization_state<>'MATERIALIZED'
           OR NEW.workflow_step_origin_node_id IS NOT NULL OR NEW.workflow_business_attempt<>1 THEN
            RAISE EXCEPTION 'workflow anchor assignment requires a planned first attempt' USING ERRCODE='23514';
        END IF;
    END IF;
    IF NEW.workflow_business_attempt=0 THEN
        IF NEW.workflow_step_origin_node_id IS NOT NULL OR NEW.workflow_captured_artifacts IS NOT NULL
           OR NEW.workflow_attempts<>'[]'::jsonb OR NEW.workflow_inputs<>'[]'::jsonb
           OR NEW.workflow_effect_intents<>'[]'::jsonb OR NEW.workflow_effect_rejoins<>'[]'::jsonb THEN
            RAISE EXCEPTION 'unassigned execution cannot own workflow provenance' USING ERRCODE='23514';
        END IF;
        RETURN NEW;
    END IF;
    IF NEW.type<>'AGENT_EXECUTION' OR NEW.materialization_state<>'MATERIALIZED'
       OR NEW.turn_id IS NULL OR NEW.agent_id IS NULL OR NOT EXISTS (
        SELECT 1 FROM control_plane.runs root
        JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id
        JOIN control_plane.runs physical ON physical.id=NEW.run_id AND physical.organization_id=root.organization_id
            AND physical.root_run_id=root.id AND physical.project_id IS NOT DISTINCT FROM root.project_id
        JOIN control_plane.session_turns turn ON turn.id=NEW.turn_id AND turn.run_id=physical.id
            AND turn.session_id=physical.session_id
        JOIN control_plane.agents agent ON agent.id=NEW.agent_id AND agent.organization_id=root.organization_id
            AND agent.project_id IS NOT DISTINCT FROM root.project_id
        CROSS JOIN LATERAL jsonb_array_elements(version.spec->'Steps') step
        WHERE root.id=NEW.root_run_id AND root.organization_id=NEW.organization_id
            AND version.spec->>'ExecutionMode'='SERVER_DAG_V1'
            AND step->>'Key'=NEW.workflow_step_key AND step->>'AgentRef'=agent.ref
    ) THEN
        RAISE EXCEPTION 'workflow physical execution does not match the pinned step' USING ERRCODE='23514';
    END IF;
    IF NEW.workflow_step_origin_node_id IS NULL THEN
        IF NEW.workflow_business_attempt<>1 THEN
            RAISE EXCEPTION 'workflow anchor must own the first business attempt' USING ERRCODE='23514';
        END IF;
    ELSE
        SELECT node.id,node.workflow_step_origin_node_id,node.workflow_business_attempt,
               node.organization_id,node.root_run_id,node.workflow_step_key,node.agent_id
            INTO STRICT origin FROM control_plane.run_nodes node
            WHERE node.id=NEW.workflow_step_origin_node_id;
        IF origin.id=NEW.id OR origin.workflow_step_origin_node_id IS NOT NULL
           OR origin.workflow_business_attempt<>1
           OR ROW(origin.organization_id,origin.root_run_id,origin.workflow_step_key,origin.agent_id)
              IS DISTINCT FROM ROW(NEW.organization_id,NEW.root_run_id,NEW.workflow_step_key,NEW.agent_id) THEN
            RAISE EXCEPTION 'workflow logical origin does not match the physical execution' USING ERRCODE='23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_workflow_physical_lineage() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.guard_workflow_physical_lineage() TO control_plane_runtime;
CREATE TRIGGER workflow_physical_lineage_guard BEFORE INSERT OR UPDATE OR DELETE ON control_plane.run_nodes
    FOR EACH ROW EXECUTE FUNCTION control_plane.guard_workflow_physical_lineage();

-- Capture удостоверяет producing RR, а не текущий каталог более позднего completion.
-- NULL означает отсутствие capture; объект с artifacts=[] — уже закрытое пустое завершение.
-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_workflow_capture() RETURNS trigger
LANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $$
DECLARE capture jsonb; entry jsonb;
BEGIN
    IF TG_OP='INSERT' THEN
        IF NEW.workflow_captured_artifacts IS NOT NULL THEN
            RAISE EXCEPTION 'workflow capture must follow completed execution' USING ERRCODE='23514';
        END IF;
        RETURN NEW;
    END IF;
    IF OLD.workflow_captured_artifacts IS NOT NULL THEN
        IF NEW.workflow_captured_artifacts IS DISTINCT FROM OLD.workflow_captured_artifacts THEN
            RAISE EXCEPTION 'workflow capture is immutable' USING ERRCODE='23514';
        END IF;
        RETURN NEW;
    END IF;
    capture:=NEW.workflow_captured_artifacts;
    IF capture IS NULL THEN RETURN NEW; END IF;
    IF NEW.workflow_business_attempt=0 OR jsonb_typeof(capture) IS DISTINCT FROM 'object'
       OR capture-ARRAY['schema','runtimeRevisionId','leaseId','generation','turnId','runtimeAttempt','businessAttempt','artifacts']<>'{}'::jsonb
       OR capture->>'schema' IS DISTINCT FROM 'kodex.workflow-capture.v1'
       OR jsonb_typeof(capture->'artifacts') IS DISTINCT FROM 'array'
       OR jsonb_array_length(capture->'artifacts')>16
       OR capture->'runtimeAttempt' IS DISTINCT FROM to_jsonb(NEW.attempt)
       OR capture->'businessAttempt' IS DISTINCT FROM to_jsonb(NEW.workflow_business_attempt)
       OR capture->>'turnId' IS DISTINCT FROM NEW.turn_id::text
       OR NOT EXISTS (
        SELECT 1 FROM control_plane.runtime_revisions revision
        JOIN control_plane.runtime_leases lease ON lease.runtime_revision_id=revision.id
            AND lease.organization_id=revision.organization_id AND lease.node_id=revision.node_id
            AND lease.run_id=revision.run_id AND lease.generation=revision.generation AND lease.state='COMPLETED'
        WHERE revision.id::text=capture->>'runtimeRevisionId' AND lease.id::text=capture->>'leaseId'
            AND to_jsonb(lease.generation)=capture->'generation'
            AND revision.organization_id=NEW.organization_id AND revision.root_run_id=NEW.root_run_id
            AND revision.node_id=NEW.id AND revision.run_id=NEW.run_id
            AND revision.turn_id=NEW.turn_id AND revision.agent_id=NEW.agent_id AND revision.attempt=NEW.attempt
       ) THEN
        RAISE EXCEPTION 'workflow capture requires exact completed runtime provenance' USING ERRCODE='23514';
    END IF;
    IF EXISTS (SELECT 1 FROM jsonb_array_elements(capture->'artifacts') item GROUP BY item->>'ref' HAVING count(*)<>1) THEN
        RAISE EXCEPTION 'workflow capture artifact references must be unique' USING ERRCODE='23514';
    END IF;
    FOR entry IN SELECT value FROM jsonb_array_elements(capture->'artifacts') LOOP
        IF jsonb_typeof(entry) IS DISTINCT FROM 'object'
           OR entry-ARRAY['ref','revisionRef','revision','digest','sizeBytes','fileName','mediaType']<>'{}'::jsonb
           OR NOT EXISTS (
            SELECT 1 FROM control_plane.artifact_history artifact
            JOIN control_plane.runs producer ON producer.id=artifact.run_id AND producer.organization_id=artifact.organization_id
                AND producer.project_id IS NOT DISTINCT FROM artifact.project_id
            JOIN control_plane.artifact_revision_content content ON content.revision_id=artifact.revision_id
                AND content.digest=artifact.digest AND content.size_bytes=artifact.size_bytes
            WHERE artifact.organization_id=NEW.organization_id AND artifact.node_id=NEW.id AND artifact.run_id=NEW.run_id
                AND artifact.source='AGENT_RESULT' AND artifact.lifecycle_state='ACTIVE'
                AND artifact.ref=entry->>'ref' AND artifact.revision_ref=entry->>'revisionRef'
                AND to_jsonb(artifact.revision)=entry->'revision' AND artifact.digest=entry->>'digest'
                AND to_jsonb(artifact.size_bytes)=entry->'sizeBytes'
                AND artifact.file_name=entry->>'fileName' AND artifact.media_type=entry->>'mediaType'
           ) THEN
            RAISE EXCEPTION 'workflow capture artifact does not match immutable content' USING ERRCODE='23514';
        END IF;
    END LOOP;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_workflow_capture() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.guard_workflow_capture() TO control_plane_runtime;
CREATE TRIGGER workflow_capture_guard BEFORE INSERT OR UPDATE ON control_plane.run_nodes
    FOR EACH ROW EXECUTE FUNCTION control_plane.guard_workflow_capture();

RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    RAISE EXCEPTION 'server workflow migration is forward-only';
END $$;
-- +goose StatementEnd
