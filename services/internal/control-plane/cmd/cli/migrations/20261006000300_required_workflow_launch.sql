-- +goose Up
SET LOCAL ROLE control_plane_owner;
-- Дочерний Workflow сохраняет собственный execution root; provenance и
-- обязательный lifecycle принадлежат отдельной связи владельца CP.
CREATE TABLE control_plane.required_workflow_launches (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^wlaunch_[A-Za-z0-9_-]{8,88}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    project_id uuid NOT NULL REFERENCES control_plane.projects(id),
    root_actor_id uuid NOT NULL REFERENCES control_plane.subjects(id),
    origin_root_run_id uuid NOT NULL REFERENCES control_plane.runs(id),
    origin_run_id uuid NOT NULL REFERENCES control_plane.runs(id),
    origin_node_id uuid NOT NULL REFERENCES control_plane.run_nodes(id),
    origin_session_id uuid NOT NULL REFERENCES control_plane.sessions(id),
    origin_turn_id uuid NOT NULL REFERENCES control_plane.session_turns(id),
    origin_runtime_revision_id uuid NOT NULL REFERENCES control_plane.runtime_revisions(id),
    origin_generation bigint NOT NULL CHECK (origin_generation > 0),
    origin_attempt integer NOT NULL CHECK (origin_attempt > 0),
    origin_input_digest text NOT NULL CHECK (origin_input_digest ~ '^[a-f0-9]{64}$'),
    origin_revision_digest text NOT NULL CHECK (origin_revision_digest ~ '^[a-f0-9]{64}$'),
    workflow_ref text NOT NULL,
    workflow_version_id uuid NOT NULL REFERENCES control_plane.workflow_versions(id),
    request_digest text NOT NULL CHECK (request_digest ~ '^[a-f0-9]{64}$'),
    child_root_run_id uuid NOT NULL UNIQUE REFERENCES control_plane.runs(id),
    proxy_node_id uuid NOT NULL UNIQUE REFERENCES control_plane.run_nodes(id),
    callback_edge_id uuid NOT NULL UNIQUE REFERENCES control_plane.run_edges(id),
    state text NOT NULL DEFAULT 'OPEN' CHECK (state IN ('OPEN','SUCCEEDED','FAILED','CANCELLED')),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    completed_at timestamptz,
    CHECK (origin_root_run_id <> child_root_run_id),
    CHECK ((state = 'OPEN') = (completed_at IS NULL)),
    UNIQUE (origin_runtime_revision_id, workflow_ref, request_digest)
);
CREATE INDEX required_workflow_launches_origin_root ON control_plane.required_workflow_launches(organization_id, origin_root_run_id, state);
CREATE INDEX required_workflow_launches_origin_run ON control_plane.required_workflow_launches(origin_run_id, state);
-- Зафиксированные координаты не перезаписываются при retry/continuation.
-- +goose StatementBegin
CREATE FUNCTION control_plane.protect_required_workflow_launch_origin() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog,control_plane AS $$
BEGIN
    IF TG_OP='INSERT' THEN
        IF NEW.state<>'OPEN' OR NEW.completed_at IS NOT NULL OR NOT EXISTS (
            SELECT 1 FROM control_plane.runtime_revisions revision
            JOIN control_plane.runs parent ON parent.id=NEW.origin_run_id
            JOIN control_plane.runs root ON root.id=NEW.origin_root_run_id
            JOIN control_plane.run_nodes node ON node.id=NEW.origin_node_id
            JOIN control_plane.runs child ON child.id=NEW.child_root_run_id
            JOIN control_plane.run_nodes proxy ON proxy.id=NEW.proxy_node_id
            JOIN control_plane.run_edges edge ON edge.id=NEW.callback_edge_id
            WHERE revision.id=NEW.origin_runtime_revision_id AND revision.organization_id=NEW.organization_id
              AND revision.project_id=NEW.project_id AND revision.root_run_id=root.id AND revision.run_id=parent.id
              AND revision.node_id=node.id AND revision.session_id=NEW.origin_session_id AND revision.turn_id=NEW.origin_turn_id
              AND revision.generation=NEW.origin_generation AND revision.attempt=NEW.origin_attempt
              AND revision.input_digest=NEW.origin_input_digest AND revision.revision_digest=NEW.origin_revision_digest
              AND parent.organization_id=NEW.organization_id AND parent.project_id=NEW.project_id
              AND parent.root_run_id=root.id AND parent.session_id=NEW.origin_session_id
              AND root.organization_id=NEW.organization_id AND root.initiated_by=NEW.root_actor_id
              AND node.organization_id=NEW.organization_id AND node.root_run_id=root.id AND node.run_id=parent.id
              AND node.turn_id=NEW.origin_turn_id AND node.attempt=NEW.origin_attempt
              AND child.organization_id=NEW.organization_id AND child.project_id=NEW.project_id
              AND child.root_run_id=child.id AND child.parent_run_id=parent.id AND child.initiated_by=NEW.root_actor_id
              AND child.target_type='WORKFLOW' AND child.target_ref=NEW.workflow_ref AND child.workflow_version_id=NEW.workflow_version_id
              AND child.source='AGENT_DELEGATION' AND proxy.organization_id=NEW.organization_id
              AND proxy.root_run_id=root.id AND proxy.run_id=parent.id AND proxy.parent_node_id=node.id
              AND edge.organization_id=NEW.organization_id AND edge.root_run_id=root.id
              AND edge.source_node_id=proxy.id AND edge.target_node_id=node.id AND edge.type='CALLBACK_TO') THEN
            RAISE EXCEPTION 'required workflow launch origin mismatch';
        END IF;
        RETURN NEW;
    END IF;
    IF TG_OP='DELETE' THEN
        IF current_user='control_plane_owner' AND EXISTS (
            SELECT 1 FROM control_plane.project_purge_context context
            JOIN control_plane.projects project ON project.id=context.project_id AND project.id=OLD.project_id AND project.organization_id=OLD.organization_id
            JOIN control_plane.project_purge_receipts receipt ON receipt.project_id=project.id AND receipt.organization_id=project.organization_id
            JOIN control_plane.project_purge_targets target ON target.transaction_id=context.transaction_id AND target.relation_id=TG_RELID
            WHERE context.transaction_id=pg_current_xact_id_if_assigned() AND context.backend_pid=pg_backend_pid()
              AND project.lifecycle='PURGE_PENDING' AND receipt.state='OBJECTS_CLEARED' AND receipt.objects_cleared_at IS NOT NULL
              AND target.row_key=jsonb_build_array(OLD.id)) THEN RETURN OLD; END IF;
        RAISE EXCEPTION 'required workflow launch origin is immutable';
    END IF;
    IF (to_jsonb(NEW) - ARRAY['state','completed_at']) IS DISTINCT FROM
       (to_jsonb(OLD) - ARRAY['state','completed_at']) OR OLD.state <> 'OPEN' THEN
        RAISE EXCEPTION 'required workflow launch origin is immutable';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER protect_required_workflow_launch_origin BEFORE INSERT OR UPDATE OR DELETE ON control_plane.required_workflow_launches
FOR EACH ROW EXECUTE FUNCTION control_plane.protect_required_workflow_launch_origin();
-- +goose StatementEnd
REVOKE DELETE ON control_plane.required_workflow_launches FROM control_plane_runtime;
GRANT SELECT,INSERT,UPDATE ON control_plane.required_workflow_launches TO control_plane_runtime;

-- Exact forward-only изменение известного purge графа вместе с новым видом.
-- +goose StatementBegin
DO $migration$
DECLARE
 v_nodes integer;v_node_hash text;v_edges integer;v_edge_hash text;v_definition text;
 v_old_nodes constant text := 'IF v_nodes <> 100 OR v_node_fingerprint <> ''41669910a543578b4ef955aefbb8a25a'' THEN';
 v_new_nodes constant text := 'IF v_nodes <> 101 OR v_node_fingerprint <> ''fd84f327b771b83de59d0839d477432c'' THEN';
 v_old_edges constant text := 'IF v_edges <> 253 OR v_edge_fingerprint <> ''372b45622a9bc15b0f10dcc5335b9883'' THEN';
 v_new_edges constant text := 'IF v_edges <> 264 OR v_edge_fingerprint <> ''149c59d0b352b832823e09f10a28f55b'' THEN';
BEGIN
 WITH RECURSIVE nodes(relation_id) AS (SELECT 'control_plane.projects'::regclass::oid UNION
  SELECT c.conrelid FROM nodes JOIN pg_constraint c ON c.contype='f' AND c.confrelid=nodes.relation_id
  JOIN pg_namespace n ON n.oid=c.connamespace AND n.nspname='control_plane')
 SELECT count(*),md5(string_agg(n.nspname||'.'||c.relname,E'\n' ORDER BY n.nspname,c.relname))
 INTO v_nodes,v_node_hash FROM nodes JOIN pg_class c ON c.oid=nodes.relation_id JOIN pg_namespace n ON n.oid=c.relnamespace;
 WITH RECURSIVE nodes(relation_id) AS (SELECT 'control_plane.projects'::regclass::oid UNION
  SELECT c.conrelid FROM nodes JOIN pg_constraint c ON c.contype='f' AND c.confrelid=nodes.relation_id
  JOIN pg_namespace n ON n.oid=c.connamespace AND n.nspname='control_plane')
 SELECT count(*),md5(string_agg(cn.nspname||'.'||cc.relname||'|'||c.conname||'|'||pn.nspname||'.'||pc.relname||'|'||c.confdeltype::text,
 E'\n' ORDER BY cn.nspname,cc.relname,c.conname)) INTO v_edges,v_edge_hash
 FROM pg_constraint c JOIN pg_class cc ON cc.oid=c.conrelid JOIN pg_namespace cn ON cn.oid=cc.relnamespace
 JOIN pg_class pc ON pc.oid=c.confrelid JOIN pg_namespace pn ON pn.oid=pc.relnamespace
 WHERE c.contype='f' AND c.confrelid IN (SELECT relation_id FROM nodes) AND c.conrelid IN (SELECT relation_id FROM nodes);
 IF v_nodes<>101 OR v_node_hash<>'fd84f327b771b83de59d0839d477432c' OR v_edges<>264 OR v_edge_hash<>'149c59d0b352b832823e09f10a28f55b' THEN
  RAISE EXCEPTION 'required workflow purge graph migration precondition failed';END IF;
 SELECT pg_get_functiondef('control_plane.purge_project_database(uuid,uuid,text)'::regprocedure) INTO v_definition;
 IF strpos(v_definition,v_old_nodes)=0 OR strpos(v_definition,v_old_edges)=0 OR
 strpos(substr(v_definition,strpos(v_definition,v_old_nodes)+length(v_old_nodes)),v_old_nodes)>0 OR
 strpos(substr(v_definition,strpos(v_definition,v_old_edges)+length(v_old_edges)),v_old_edges)>0 THEN
  RAISE EXCEPTION 'required workflow purge function migration precondition failed';END IF;
 EXECUTE replace(replace(v_definition,v_old_nodes,v_new_nodes),v_old_edges,v_new_edges);
END;
$migration$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'required workflow launch migration is forward-only'; END $$;
-- +goose StatementEnd
