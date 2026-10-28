-- +goose Up
SET ROLE control_plane_owner;

CREATE TABLE control_plane.repository_bundle_publications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^rbp_[A-Za-z0-9_-]{8,89}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id) DEFERRABLE INITIALLY IMMEDIATE,
    project_id uuid NOT NULL REFERENCES control_plane.projects(id) DEFERRABLE INITIALLY IMMEDIATE,
    root_actor_id uuid NOT NULL REFERENCES control_plane.subjects(id) DEFERRABLE INITIALLY IMMEDIATE,
    root_run_id uuid NOT NULL REFERENCES control_plane.runs(id) DEFERRABLE INITIALLY IMMEDIATE,
    origin_run_id uuid NOT NULL REFERENCES control_plane.runs(id) DEFERRABLE INITIALLY IMMEDIATE,
    origin_node_id uuid NOT NULL REFERENCES control_plane.run_nodes(id) DEFERRABLE INITIALLY IMMEDIATE,
    origin_runtime_revision_id uuid NOT NULL REFERENCES control_plane.runtime_revisions(id) DEFERRABLE INITIALLY IMMEDIATE,
    origin_lease_ref text NOT NULL,
    origin_generation bigint NOT NULL CHECK (origin_generation > 0),
    connection_id uuid NOT NULL REFERENCES control_plane.integration_connections(id) DEFERRABLE INITIALLY IMMEDIATE,
    grant_id uuid NOT NULL REFERENCES control_plane.integration_grants(id) DEFERRABLE INITIALLY IMMEDIATE,
    credential_revision_id uuid NOT NULL REFERENCES control_plane.integration_credential_revisions(id) DEFERRABLE INITIALLY IMMEDIATE,
    owner_gate_id uuid NOT NULL UNIQUE REFERENCES control_plane.owner_gates(id) DEFERRABLE INITIALLY IMMEDIATE,
    integration_invocation_id uuid NOT NULL UNIQUE REFERENCES control_plane.integration_invocations(id) DEFERRABLE INITIALLY IMMEDIATE,
    idempotency_key text NOT NULL CHECK (length(idempotency_key) BETWEEN 8 AND 128),
    input_sha256 text NOT NULL CHECK (input_sha256 ~ '^[0-9a-f]{64}$'),
    snapshot_sha256 text NOT NULL CHECK (snapshot_sha256 ~ '^[0-9a-f]{64}$'),
    approval_digest text NOT NULL CHECK (approval_digest ~ '^[0-9a-f]{64}$'),
    input_snapshot jsonb NOT NULL CHECK (jsonb_typeof(input_snapshot)='object' AND octet_length(input_snapshot::text) <= 524288),
    manifest_metadata jsonb NOT NULL CHECK (jsonb_typeof(manifest_metadata)='object' AND octet_length(manifest_metadata::text)<=196608),
    manifest_sha256 text NOT NULL CHECK (manifest_sha256 ~ '^[0-9a-f]{64}$'),
    archive_revision_ref text NOT NULL CHECK (archive_revision_ref ~ '^arv_[A-Za-z0-9_-]{8,89}$'),
    manifest_revision_ref text NOT NULL CHECK (manifest_revision_ref ~ '^arv_[A-Za-z0-9_-]{8,89}$'),
    archive_sha256 text NOT NULL CHECK (archive_sha256 ~ '^[0-9a-f]{64}$'),
    base_commit_sha text NOT NULL CHECK (base_commit_sha ~ '^[0-9a-f]{40}$'),
    repository_ref text NOT NULL CHECK (length(repository_ref) BETWEEN 3 AND 201 AND repository_ref=lower(repository_ref)),
    proposal_branch text NOT NULL UNIQUE CHECK (proposal_branch='kodex/bundle/'||ref),
    version bigint NOT NULL DEFAULT 1 CHECK (version>0),
    state text NOT NULL DEFAULT 'WAITING_APPROVAL' CHECK (state IN ('WAITING_APPROVAL','QUEUED','CLAIMED','EFFECT_STARTED','UNKNOWN_OUTCOME','SUCCEEDED','REJECTED','CANCELLED','EXPIRED','FAILED')),
    approver_id uuid REFERENCES control_plane.subjects(id) DEFERRABLE INITIALLY IMMEDIATE,
    approved_at timestamptz,
    deadline timestamptz,
    attempt bigint NOT NULL DEFAULT 0 CHECK (attempt BETWEEN 0 AND 3),
    claim_generation bigint NOT NULL DEFAULT 0 CHECK (claim_generation>=0),
    claimant text NOT NULL DEFAULT '' CHECK (length(claimant)<=128),
    fence text NOT NULL DEFAULT '' CHECK (length(fence)<=128),
    lease_expires_at timestamptz,
    invocation_lease_ref text NOT NULL DEFAULT '' CHECK (length(invocation_lease_ref)<=96),
    candidate_commit_sha text NOT NULL DEFAULT '' CHECK (candidate_commit_sha='' OR candidate_commit_sha ~ '^[0-9a-f]{40}$'),
    candidate_tree_sha text NOT NULL DEFAULT '' CHECK (candidate_tree_sha='' OR candidate_tree_sha ~ '^[0-9a-f]{40}$'),
    effect_started_at timestamptz,
    confirmed_at timestamptz,
    failure_code text NOT NULL DEFAULT '' CHECK (failure_code IN ('','UNAVAILABLE','AUTHORITY_CHANGED','CONTENT_INVALID','BASE_INVALID','DEADLINE_EXCEEDED','BRANCH_CONFLICT','OUTCOME_UNCONFIRMED')),
    continuation_state text NOT NULL DEFAULT '' CHECK (continuation_state IN ('','BLOCKED','CANCELLED')),
    continuation_code text NOT NULL DEFAULT '' CHECK (continuation_code IN ('','CALLBACK_INPUT_TOO_LARGE','CONTINUATION_AUTHORITY_CHANGED')),
    continuation_node_ref text NOT NULL DEFAULT '' CHECK (continuation_node_ref='' OR continuation_node_ref ~ '^nod_[A-Za-z0-9_-]{8,89}$'),
    receipts jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(receipts)='object' AND octet_length(receipts::text)<=262144),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at timestamptz NOT NULL DEFAULT clock_timestamp()+interval '24 hours',
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    completed_at timestamptz,
    UNIQUE (organization_id,origin_runtime_revision_id,origin_node_id,idempotency_key),
    UNIQUE (organization_id,origin_runtime_revision_id,origin_node_id),
    CHECK ((approved_at IS NULL AND approver_id IS NULL AND deadline IS NULL) OR (approved_at IS NOT NULL AND approver_id IS NOT NULL AND deadline IS NOT NULL)),
    CHECK (state NOT IN ('QUEUED','CLAIMED','EFFECT_STARTED','UNKNOWN_OUTCOME','SUCCEEDED') OR approved_at IS NOT NULL),
    CHECK (state NOT IN ('CLAIMED','EFFECT_STARTED','UNKNOWN_OUTCOME') OR claim_generation>0),
    CHECK (state NOT IN ('CLAIMED','EFFECT_STARTED') OR claimant<>'' AND fence<>'' AND lease_expires_at IS NOT NULL),
    CHECK (effect_started_at IS NULL OR candidate_commit_sha<>'' AND candidate_tree_sha<>''),
    CHECK (state NOT IN ('EFFECT_STARTED','UNKNOWN_OUTCOME') OR effect_started_at IS NOT NULL),
    CHECK (state<>'SUCCEEDED' OR confirmed_at IS NOT NULL AND effect_started_at IS NOT NULL AND completed_at IS NOT NULL),
    CHECK (confirmed_at IS NULL OR state='SUCCEEDED'),
    CHECK (continuation_state<>'BLOCKED' OR state IN ('SUCCEEDED','REJECTED','CANCELLED','EXPIRED','FAILED')),
    CHECK ((continuation_state='' AND continuation_code='' AND continuation_node_ref='')
       OR (continuation_state='BLOCKED' AND continuation_code IN ('CALLBACK_INPUT_TOO_LARGE','CONTINUATION_AUTHORITY_CHANGED') AND continuation_node_ref<>'')
       OR (continuation_state='CANCELLED' AND continuation_code='' AND continuation_node_ref<>''))
);

CREATE INDEX repository_bundle_publications_due
    ON control_plane.repository_bundle_publications(organization_id,updated_at,id)
    WHERE state IN ('WAITING_APPROVAL','QUEUED','CLAIMED','EFFECT_STARTED','UNKNOWN_OUTCOME');
CREATE INDEX repository_bundle_publications_archive_retention
    ON control_plane.repository_bundle_publications(archive_revision_ref)
    WHERE state IN ('WAITING_APPROVAL','QUEUED','CLAIMED','EFFECT_STARTED','UNKNOWN_OUTCOME');
CREATE INDEX repository_bundle_publications_manifest_retention
    ON control_plane.repository_bundle_publications(manifest_revision_ref)
    WHERE state IN ('WAITING_APPROVAL','QUEUED','CLAIMED','EFFECT_STARTED','UNKNOWN_OUTCOME');
CREATE INDEX repository_bundle_publications_project
    ON control_plane.repository_bundle_publications(organization_id,project_id,created_at,id);
CREATE INDEX repository_bundle_publications_root_pending
    ON control_plane.repository_bundle_publications(root_run_id,origin_run_id)
    WHERE state IN ('WAITING_APPROVAL','QUEUED','CLAIMED','EFFECT_STARTED','UNKNOWN_OUTCOME');
CREATE INDEX repository_bundle_publications_run_history
    ON control_plane.repository_bundle_publications(organization_id,origin_run_id,created_at DESC,id DESC);
CREATE INDEX repository_bundle_publications_origin_node
    ON control_plane.repository_bundle_publications(organization_id,origin_node_id);
CREATE INDEX repository_bundle_publications_blocked_continuation
    ON control_plane.repository_bundle_publications(root_run_id,origin_run_id) WHERE continuation_state='BLOCKED';
CREATE UNIQUE INDEX repository_bundle_publications_one_pending_source
    ON control_plane.repository_bundle_publications(organization_id,project_id,repository_ref,archive_sha256,manifest_sha256,base_commit_sha)
    WHERE state IN ('WAITING_APPROVAL','QUEUED','CLAIMED','EFFECT_STARTED','UNKNOWN_OUTCOME');

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_repository_bundle_publication() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='DELETE' THEN
        IF current_user='control_plane_owner' AND OLD.state IN ('SUCCEEDED','REJECTED','CANCELLED','EXPIRED','FAILED') AND OLD.continuation_state<>'BLOCKED' AND EXISTS (
            SELECT 1 FROM control_plane.project_purge_context context
            WHERE context.transaction_id=pg_current_xact_id_if_assigned() AND context.backend_pid=pg_backend_pid()
              AND context.project_id=OLD.project_id
        ) THEN RETURN OLD; END IF;
        RAISE EXCEPTION 'repository bundle publication deletion is not authorized' USING ERRCODE='42501';
    END IF;
    IF OLD.state IN ('SUCCEEDED','REJECTED','CANCELLED','EXPIRED','FAILED') THEN
        IF (to_jsonb(NEW)-ARRAY['version','updated_at','continuation_state','continuation_code','continuation_node_ref','receipts'])
             IS DISTINCT FROM (to_jsonb(OLD)-ARRAY['version','updated_at','continuation_state','continuation_code','continuation_node_ref','receipts'])
           OR NEW.version<>OLD.version+1 OR NOT NEW.receipts @> OLD.receipts
           OR (OLD.receipts ? 'COMPLETE' AND (NEW.receipts->'COMPLETE') IS DISTINCT FROM (OLD.receipts->'COMPLETE'))
           OR (NEW.receipts-'COMPLETE') IS DISTINCT FROM (OLD.receipts-'COMPLETE')
           OR NOT ((ROW(NEW.continuation_state,NEW.continuation_code,NEW.continuation_node_ref)
                     IS NOT DISTINCT FROM ROW(OLD.continuation_state,OLD.continuation_code,OLD.continuation_node_ref))
             OR (OLD.continuation_state='' AND NEW.continuation_state='BLOCKED')
             OR (OLD.continuation_state='BLOCKED' AND NEW.continuation_state='CANCELLED' AND NEW.continuation_node_ref=OLD.continuation_node_ref)) THEN
            RAISE EXCEPTION 'repository bundle terminal receipt immutable fence violation';
        END IF;
        RETURN NEW;
    END IF;
    IF
       ROW(NEW.id,NEW.ref,NEW.organization_id,NEW.project_id,NEW.root_actor_id,NEW.root_run_id,
           NEW.origin_run_id,NEW.origin_node_id,NEW.origin_runtime_revision_id,NEW.origin_lease_ref,NEW.origin_generation,
           NEW.connection_id,NEW.grant_id,NEW.credential_revision_id,NEW.owner_gate_id,NEW.integration_invocation_id,NEW.idempotency_key,
           NEW.input_sha256,NEW.snapshot_sha256,NEW.approval_digest,NEW.input_snapshot,NEW.manifest_metadata,NEW.manifest_sha256,
           NEW.archive_revision_ref,NEW.manifest_revision_ref,NEW.archive_sha256,NEW.base_commit_sha,NEW.repository_ref,NEW.proposal_branch,NEW.created_at,NEW.expires_at)
       IS DISTINCT FROM
       ROW(OLD.id,OLD.ref,OLD.organization_id,OLD.project_id,OLD.root_actor_id,OLD.root_run_id,
           OLD.origin_run_id,OLD.origin_node_id,OLD.origin_runtime_revision_id,OLD.origin_lease_ref,OLD.origin_generation,
           OLD.connection_id,OLD.grant_id,OLD.credential_revision_id,OLD.owner_gate_id,OLD.integration_invocation_id,OLD.idempotency_key,
           OLD.input_sha256,OLD.snapshot_sha256,OLD.approval_digest,OLD.input_snapshot,OLD.manifest_metadata,OLD.manifest_sha256,
           OLD.archive_revision_ref,OLD.manifest_revision_ref,OLD.archive_sha256,OLD.base_commit_sha,OLD.repository_ref,OLD.proposal_branch,OLD.created_at,OLD.expires_at) OR
       NEW.version<>OLD.version+1 OR NEW.claim_generation<OLD.claim_generation OR NEW.attempt<OLD.attempt OR
       NOT NEW.receipts @> OLD.receipts OR
       (OLD.approved_at IS NOT NULL AND ROW(NEW.approver_id,NEW.approved_at,NEW.deadline) IS DISTINCT FROM ROW(OLD.approver_id,OLD.approved_at,OLD.deadline)) OR
       (OLD.effect_started_at IS NOT NULL AND ROW(NEW.effect_started_at,NEW.candidate_commit_sha,NEW.candidate_tree_sha) IS DISTINCT FROM ROW(OLD.effect_started_at,OLD.candidate_commit_sha,OLD.candidate_tree_sha)) THEN
        RAISE EXCEPTION 'repository bundle publication immutable fence violation';
    END IF;
    IF NOT (
        (OLD.state='WAITING_APPROVAL' AND NEW.state IN ('QUEUED','REJECTED','CANCELLED','EXPIRED')) OR
        (OLD.state='QUEUED' AND NEW.state IN ('CLAIMED','CANCELLED','EXPIRED','FAILED')) OR
        (OLD.state='CLAIMED' AND NEW.state IN ('CLAIMED','EFFECT_STARTED','CANCELLED','EXPIRED','FAILED')) OR
        (OLD.state='EFFECT_STARTED' AND NEW.state IN ('EFFECT_STARTED','UNKNOWN_OUTCOME','SUCCEEDED')) OR
        (OLD.state='UNKNOWN_OUTCOME' AND NEW.state IN ('UNKNOWN_OUTCOME','SUCCEEDED'))
    ) THEN
        RAISE EXCEPTION 'repository bundle publication transition rejected';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER repository_bundle_publication_guard
    BEFORE UPDATE OR DELETE ON control_plane.repository_bundle_publications
    FOR EACH ROW EXECUTE FUNCTION control_plane.guard_repository_bundle_publication();

GRANT SELECT,INSERT,UPDATE ON control_plane.repository_bundle_publications TO control_plane_runtime;

-- Retention является consumer state, не новым permission. Сохраняется вся
-- прежняя owner-функция и добавляются только exact pending publication pins.
-- Native purge сохраняет HEAD lock и VOLATILE trigger recheck до S3 Delete.
-- +goose StatementBegin
DO $migration$
DECLARE definition text; marker text; replacement text;
BEGIN
 SELECT pg_get_functiondef('control_plane.artifact_has_retained_revisions(uuid)'::regprocedure) INTO definition;
 marker:=E'    SELECT EXISTS (\n        SELECT 1 FROM control_plane.artifact_heads artifact';
 IF (length(definition)-length(replace(definition,marker,'')))/length(marker)<>1 THEN
   RAISE EXCEPTION 'repository bundle artifact retention source changed'; END IF;
 replacement:=$patch$    SELECT EXISTS (
        SELECT 1 FROM control_plane.repository_bundle_publications publication
        JOIN control_plane.artifact_revisions revision ON revision.artifact_id=p_artifact_id
          AND revision.ref IN (publication.archive_revision_ref,publication.manifest_revision_ref)
        JOIN control_plane.artifact_heads retained ON retained.id=revision.artifact_id
          AND retained.organization_id=publication.organization_id AND retained.project_id=publication.project_id
        WHERE publication.state IN ('WAITING_APPROVAL','QUEUED','CLAIMED','EFFECT_STARTED','UNKNOWN_OUTCOME')
    ) OR EXISTS (
        SELECT 1 FROM control_plane.artifact_heads artifact$patch$;
 EXECUTE replace(definition,marker,replacement);
END $migration$;
-- +goose StatementEnd

-- Baseline owner-граф сверяется без новой таблицы; разрешено добавить только
-- собственную leaf relation. Чужое изменение schema не переопределяет guard.
-- +goose StatementBegin
DO $migration$
DECLARE definition text; old_nodes text; old_edges text; nodes_count integer; nodes_hash text;
        edges_count integer; edges_hash text; baseline_nodes oid[]; all_nodes oid[];
BEGIN
 SELECT pg_get_functiondef('control_plane.purge_project_database(uuid,uuid,text)'::regprocedure) INTO definition;
 old_nodes:=substring(definition FROM 'IF v_nodes <> [0-9]+ OR v_node_fingerprint <> ''[a-f0-9]{32}'' THEN');
 old_edges:=substring(definition FROM 'IF v_edges <> [0-9]+ OR v_edge_fingerprint <> ''[a-f0-9]{32}'' THEN');
 IF old_nodes IS DISTINCT FROM 'IF v_nodes <> 104 OR v_node_fingerprint <> ''5cbc6bd6bb4a8508ffd75ed4b1dd6189'' THEN'
    OR old_edges IS DISTINCT FROM 'IF v_edges <> 276 OR v_edge_fingerprint <> ''75343e68e299c44be699718099c79527'' THEN' THEN
   RAISE EXCEPTION 'repository bundle purge graph source changed'; END IF;
 WITH RECURSIVE nodes(relation_id) AS (
   SELECT 'control_plane.projects'::regclass::oid UNION
   SELECT c.conrelid FROM nodes n JOIN pg_constraint c ON c.confrelid=n.relation_id AND c.contype='f'
     AND c.connamespace='control_plane'::regnamespace AND c.conrelid<>'control_plane.repository_bundle_publications'::regclass
 ) SELECT array_agg(relation_id) INTO baseline_nodes FROM nodes;
 SELECT count(*),md5(string_agg(n.nspname||'.'||c.relname,E'\n' ORDER BY n.nspname,c.relname))
 INTO nodes_count,nodes_hash FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE c.oid=ANY(baseline_nodes);
 SELECT count(*),md5(string_agg(cn.nspname||'.'||cc.relname||'|'||c.conname||'|'||pn.nspname||'.'||pc.relname||'|'||c.confdeltype::text,
 E'\n' ORDER BY cn.nspname,cc.relname,c.conname)) INTO edges_count,edges_hash
 FROM pg_constraint c JOIN pg_class cc ON cc.oid=c.conrelid JOIN pg_namespace cn ON cn.oid=cc.relnamespace
 JOIN pg_class pc ON pc.oid=c.confrelid JOIN pg_namespace pn ON pn.oid=pc.relnamespace
 WHERE c.contype='f' AND c.confrelid=ANY(baseline_nodes) AND c.conrelid=ANY(baseline_nodes);
 IF nodes_count<>104 OR nodes_hash<>'5cbc6bd6bb4a8508ffd75ed4b1dd6189' OR edges_count<>276 OR edges_hash<>'75343e68e299c44be699718099c79527' THEN
   RAISE EXCEPTION 'repository bundle purge baseline graph changed'; END IF;
 IF EXISTS (SELECT 1 FROM pg_constraint WHERE contype='f' AND confrelid='control_plane.repository_bundle_publications'::regclass)
    OR (SELECT count(*) FROM pg_constraint WHERE contype='f' AND conrelid='control_plane.repository_bundle_publications'::regclass)<>13 THEN
   RAISE EXCEPTION 'repository bundle purge leaf graph changed'; END IF;
 all_nodes:=array_append(baseline_nodes,'control_plane.repository_bundle_publications'::regclass::oid);
 SELECT count(*),md5(string_agg(n.nspname||'.'||c.relname,E'\n' ORDER BY n.nspname,c.relname))
 INTO nodes_count,nodes_hash FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE c.oid=ANY(all_nodes);
 SELECT count(*),md5(string_agg(cn.nspname||'.'||cc.relname||'|'||c.conname||'|'||pn.nspname||'.'||pc.relname||'|'||c.confdeltype::text,
 E'\n' ORDER BY cn.nspname,cc.relname,c.conname)) INTO edges_count,edges_hash
 FROM pg_constraint c JOIN pg_class cc ON cc.oid=c.conrelid JOIN pg_namespace cn ON cn.oid=cc.relnamespace
 JOIN pg_class pc ON pc.oid=c.confrelid JOIN pg_namespace pn ON pn.oid=pc.relnamespace
 WHERE c.contype='f' AND c.confrelid=ANY(all_nodes) AND c.conrelid=ANY(all_nodes);
 definition:=replace(replace(definition,old_nodes,format('IF v_nodes <> %s OR v_node_fingerprint <> %L THEN',nodes_count,nodes_hash)),
                      old_edges,format('IF v_edges <> %s OR v_edge_fingerprint <> %L THEN',edges_count,edges_hash));
 EXECUTE definition;
END $migration$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION control_plane.repository_bundle_callback_pending(p_tenant uuid,p_child uuid)
RETURNS boolean LANGUAGE sql STABLE SECURITY INVOKER SET search_path=pg_catalog AS $$
 SELECT EXISTS (SELECT 1 FROM control_plane.repository_bundle_publications publication
   WHERE publication.organization_id=p_tenant AND publication.origin_run_id=p_child)
 AND (EXISTS (SELECT 1 FROM control_plane.repository_bundle_publications publication
       WHERE publication.organization_id=p_tenant AND publication.origin_run_id=p_child
         AND (publication.state IN ('WAITING_APPROVAL','QUEUED','CLAIMED','EFFECT_STARTED','UNKNOWN_OUTCOME') OR publication.continuation_state='BLOCKED'))
   OR EXISTS (SELECT 1 FROM control_plane.run_nodes pending
       WHERE pending.organization_id=p_tenant AND pending.run_id=p_child AND pending.type='AGENT_EXECUTION'
         AND pending.state IN ('QUEUED','RUNNING','WAITING'))
   OR EXISTS (SELECT 1 FROM control_plane.run_nodes origin
       JOIN control_plane.run_edges callback ON callback.organization_id=p_tenant AND callback.root_run_id=origin.root_run_id
         AND callback.target_node_id=origin.id AND callback.type='CALLBACK_TO'
       WHERE origin.organization_id=p_tenant AND origin.run_id=p_child
         AND NOT EXISTS (SELECT 1 FROM control_plane.callback_receipts receipt WHERE receipt.callback_edge_id=callback.id)))
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.repository_bundle_callback_pending(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.repository_bundle_callback_pending(uuid,uuid) TO control_plane_runtime;

-- Callback ancestry belongs only to this operation; generic integration continuations keep their existing policy.
-- +goose StatementBegin
CREATE FUNCTION control_plane.repository_bundle_callback_origin(p_tenant uuid,p_child uuid,p_revision uuid)
RETURNS TABLE(callback_edge_id uuid,callback_edge_ref text,parent_node_id uuid,parent_node_ref text,parent_run_id uuid,
              original_node_id uuid,final_node_id uuid,revision_ref text,attempt integer,generation bigint,eligible boolean)
LANGUAGE sql STABLE SECURITY INVOKER SET search_path=pg_catalog AS $$
 WITH RECURSIVE final AS (
   SELECT node.id,node.root_run_id,node.run_id,node.agent_id,node.attempt,node.turn_id,
          child.session_id,revision.ref AS revision_ref,revision.generation
   FROM control_plane.runtime_revisions revision
   JOIN control_plane.run_nodes node ON node.id=revision.node_id AND node.organization_id=revision.organization_id
     AND node.run_id=revision.run_id AND node.root_run_id=revision.root_run_id AND node.agent_id=revision.agent_id
     AND node.attempt=revision.attempt AND node.turn_id IS NOT DISTINCT FROM revision.turn_id AND node.state='SUCCEEDED'
   JOIN control_plane.runs child ON child.id=node.run_id AND child.organization_id=revision.organization_id
     AND child.id=p_child AND child.id<>child.root_run_id
   JOIN control_plane.session_turns turn ON turn.id=node.turn_id AND turn.run_id=child.id AND turn.session_id=child.session_id
   WHERE revision.organization_id=p_tenant AND revision.id=p_revision
 ), chain AS (
   SELECT final.id,final.root_run_id,final.run_id,final.agent_id,final.attempt,0 AS depth,false AS publication_witness FROM final
   UNION ALL
   SELECT previous.id,previous.root_run_id,previous.run_id,previous.agent_id,previous.attempt,current_node.depth+1,
     current_node.publication_witness OR EXISTS (
       SELECT 1 FROM control_plane.repository_bundle_publications publication
       JOIN control_plane.runtime_revisions proof ON proof.id=publication.origin_runtime_revision_id
         AND proof.organization_id=p_tenant AND proof.run_id=previous.run_id AND proof.node_id=previous.id
         AND proof.root_run_id=previous.root_run_id AND proof.agent_id=previous.agent_id
         AND proof.attempt=previous.attempt AND proof.turn_id IS NOT DISTINCT FROM previous.turn_id
         AND proof.generation=publication.origin_generation
       WHERE publication.organization_id=p_tenant AND publication.origin_node_id=previous.id
         AND publication.origin_run_id=previous.run_id AND publication.root_run_id=previous.root_run_id
         AND publication.state IN ('SUCCEEDED','REJECTED','CANCELLED','EXPIRED','FAILED'))
   FROM chain current_node
   JOIN control_plane.run_edges edge ON edge.organization_id=p_tenant AND edge.root_run_id=current_node.root_run_id
     AND edge.target_node_id=current_node.id AND edge.type='CONTINUES'
   JOIN control_plane.run_nodes previous ON previous.id=edge.source_node_id AND previous.organization_id=p_tenant
     AND previous.run_id=current_node.run_id AND previous.root_run_id=current_node.root_run_id
     AND previous.agent_id=current_node.agent_id AND previous.attempt+1=current_node.attempt AND previous.state='SUCCEEDED'
   JOIN control_plane.session_turns turn ON turn.id=previous.turn_id AND turn.run_id=previous.run_id
     AND turn.session_id=(SELECT session_id FROM final)
   WHERE EXISTS (SELECT 1 FROM control_plane.runtime_revisions proof
     JOIN control_plane.runtime_leases lease ON lease.runtime_revision_id=proof.id AND lease.organization_id=p_tenant
       AND lease.node_id=previous.id AND lease.run_id=previous.run_id AND lease.generation=proof.generation AND lease.state='COMPLETED'
     WHERE proof.organization_id=p_tenant AND proof.run_id=previous.run_id AND proof.node_id=previous.id
       AND proof.root_run_id=previous.root_run_id AND proof.agent_id=previous.agent_id
       AND proof.attempt=previous.attempt AND proof.turn_id IS NOT DISTINCT FROM previous.turn_id)
 )
 SELECT callback.id,callback.ref,parent_node.id,parent_node.ref,parent_run.id,source.id,
        final.id,final.revision_ref,final.attempt,final.generation,
        root.state IN ('RUNNING','WAITING_HUMAN') AND actor.active AND project.lifecycle='ACTIVE'
          AND parent_agent.enabled AND parent_agent.state='READY'
          AND NOT control_plane.repository_bundle_callback_pending(p_tenant,p_child)
   FROM chain origin CROSS JOIN final
   JOIN control_plane.run_nodes source ON source.id=origin.id AND source.organization_id=p_tenant
   JOIN control_plane.runs child ON child.id=p_child AND child.organization_id=p_tenant AND child.id=source.run_id
   JOIN control_plane.run_edges callback ON callback.organization_id=p_tenant AND callback.root_run_id=origin.root_run_id
     AND callback.source_node_id=source.id AND callback.type='CALLBACK_TO'
   JOIN control_plane.run_nodes parent_node ON parent_node.id=callback.target_node_id AND parent_node.organization_id=p_tenant
     AND parent_node.root_run_id=origin.root_run_id
   JOIN control_plane.runs parent_run ON parent_run.id=parent_node.run_id AND parent_run.organization_id=p_tenant
     AND parent_run.project_id=child.project_id AND parent_run.initiated_by=child.initiated_by
   JOIN control_plane.runs root ON root.id=origin.root_run_id AND root.organization_id=p_tenant
   JOIN control_plane.subjects actor ON actor.id=root.initiated_by AND actor.organization_id=p_tenant
   JOIN control_plane.projects project ON project.id=child.project_id AND project.organization_id=p_tenant
   JOIN control_plane.agents parent_agent ON parent_agent.id=parent_node.agent_id AND parent_agent.organization_id=p_tenant
   WHERE origin.depth>0 AND origin.publication_witness
     AND child.state IN ('SUCCEEDED','FAILED','CANCELLED')
     AND EXISTS (SELECT 1 FROM control_plane.run_edges delegation WHERE delegation.organization_id=p_tenant
       AND delegation.root_run_id=origin.root_run_id AND delegation.type='DELEGATED_TO'
       AND delegation.source_node_id=parent_node.id AND delegation.target_node_id=source.id)
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.repository_bundle_callback_origin(uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.repository_bundle_callback_origin(uuid,uuid,uuid) TO control_plane_runtime;

-- Aggregate counts are complete; latest is explicitly one receipt, not a silently truncated list of refs.
-- +goose StatementBegin
CREATE FUNCTION control_plane.repository_bundle_callback_outcome(p_tenant uuid,p_child uuid)
RETURNS jsonb LANGUAGE sql STABLE SECURITY INVOKER SET search_path=pg_catalog AS $$
 SELECT CASE WHEN count(*)=0 THEN NULL ELSE jsonb_build_object(
   'format',1,'kind','REPOSITORY_BUNDLE_PUBLICATION_OUTCOME','total',count(*),
   'succeeded',count(*) FILTER (WHERE publication.state='SUCCEEDED'),
   'unsuccessful',count(*) FILTER (WHERE publication.state IN ('REJECTED','CANCELLED','EXPIRED','FAILED')),
   'pending',count(*) FILTER (WHERE publication.state IN ('WAITING_APPROVAL','QUEUED','CLAIMED','EFFECT_STARTED','UNKNOWN_OUTCOME')),
   'allSucceeded',bool_and(publication.state='SUCCEEDED'),
   'latest',(SELECT jsonb_build_object('ref',latest.ref,'version',latest.version,'state',latest.state,'failureCode',latest.failure_code,
       'invocationRef',invocation.ref,'gateRef',gate.ref,'baseCommitSha',latest.base_commit_sha,
       'commitSha',latest.candidate_commit_sha,'treeSha',latest.candidate_tree_sha,'proposalBranch',latest.proposal_branch,
       'manifestSha256',latest.manifest_sha256,'archiveSha256',latest.archive_sha256,
       'continuationState',latest.continuation_state,'continuationCode',latest.continuation_code,'continuationNodeRef',latest.continuation_node_ref)
     FROM control_plane.repository_bundle_publications latest
     JOIN control_plane.runs latest_child ON latest_child.id=latest.origin_run_id AND latest_child.organization_id=p_tenant
       AND latest_child.root_run_id=latest.root_run_id AND latest_child.project_id=latest.project_id
     JOIN control_plane.integration_invocations invocation ON invocation.id=latest.integration_invocation_id AND invocation.organization_id=p_tenant
     JOIN control_plane.owner_gates gate ON gate.id=latest.owner_gate_id AND gate.organization_id=p_tenant
     WHERE latest.organization_id=p_tenant AND latest.origin_run_id=p_child
     ORDER BY latest.created_at DESC,latest.id DESC LIMIT 1)
   ) END
 FROM control_plane.repository_bundle_publications publication
 JOIN control_plane.runs child ON child.id=publication.origin_run_id AND child.organization_id=p_tenant
   AND child.root_run_id=publication.root_run_id AND child.project_id=publication.project_id
 WHERE publication.organization_id=p_tenant AND publication.origin_run_id=p_child
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.repository_bundle_callback_outcome(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.repository_bundle_callback_outcome(uuid,uuid) TO control_plane_runtime;

RESET ROLE;
