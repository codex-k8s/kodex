-- +goose Up
SET ROLE control_plane_owner;
SET LOCAL lock_timeout = '8s';
SET LOCAL statement_timeout = '30s';

-- Work существует до provider session ID. Его writer и удаление принадлежат
-- owner-транзакции; RWO, lease expiry и отсутствие Pod не являются join proof.
CREATE TABLE control_plane.session_workspace_storage (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^wks_[A-Za-z0-9_-]{8,89}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id) DEFERRABLE INITIALLY IMMEDIATE,
    project_id uuid NOT NULL REFERENCES control_plane.projects(id) DEFERRABLE INITIALLY IMMEDIATE,
    session_id uuid NOT NULL UNIQUE REFERENCES control_plane.sessions(id) DEFERRABLE INITIALLY IMMEDIATE,
    agent_id uuid NOT NULL REFERENCES control_plane.agents(id) DEFERRABLE INITIALLY IMMEDIATE,
    generation bigint NOT NULL DEFAULT 1 CHECK (generation > 0),
    state text NOT NULL CHECK (state IN (
        'EMPTY','WRITER_PENDING','WRITING','QUIESCING','CHECKPOINT_PENDING',
        'CHECKPOINT_READY','DELETE_READY','ARCHIVED','RESTORE_READY','RESTORING',
        'RESTORED','BLOCKED','PURGED'
    )),
    writer_lease_id uuid REFERENCES control_plane.runtime_leases(id) DEFERRABLE INITIALLY IMMEDIATE,
    writer_acknowledgement text CHECK (writer_acknowledgement IS NULL OR octet_length(writer_acknowledgement) BETWEEN 1 AND 2048),
    quiescence_record text CHECK (quiescence_record IS NULL OR octet_length(quiescence_record) BETWEEN 1 AND 2048),
    source_archive_id uuid REFERENCES control_plane.session_archives(id) DEFERRABLE INITIALLY IMMEDIATE,
    current_archive_id uuid REFERENCES control_plane.session_archives(id) DEFERRABLE INITIALLY IMMEDIATE,
    destructive_task_id uuid REFERENCES control_plane.session_archive_tasks(id) DEFERRABLE INITIALLY IMMEDIATE,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT session_workspace_writer_shape CHECK (
        (state IN ('EMPTY','RESTORE_READY','RESTORING','RESTORED') AND writer_acknowledgement IS NULL AND quiescence_record IS NULL)
        OR (state='WRITER_PENDING' AND writer_lease_id IS NOT NULL AND writer_acknowledgement IS NULL AND quiescence_record IS NULL)
        OR (state IN ('WRITING','QUIESCING') AND writer_lease_id IS NOT NULL AND writer_acknowledgement IS NOT NULL AND quiescence_record IS NULL)
        OR (state IN ('CHECKPOINT_PENDING','CHECKPOINT_READY','DELETE_READY','ARCHIVED') AND writer_lease_id IS NOT NULL AND writer_acknowledgement IS NOT NULL AND quiescence_record IS NOT NULL)
        OR state IN ('BLOCKED','PURGED')
    ),
    CONSTRAINT session_workspace_checkpoint_required CHECK (
        state NOT IN ('CHECKPOINT_READY','DELETE_READY','ARCHIVED') OR current_archive_id IS NOT NULL
    ),
    CONSTRAINT session_workspace_restore_required CHECK (
        state NOT IN ('RESTORE_READY','RESTORING','RESTORED') OR source_archive_id IS NOT NULL
    ),
    CONSTRAINT session_workspace_destructive_required CHECK (
        (state='DELETE_READY')=(destructive_task_id IS NOT NULL)
    )
);

CREATE INDEX session_workspace_checkpoint_candidates
    ON control_plane.session_workspace_storage(updated_at,id)
    WHERE state IN ('CHECKPOINT_PENDING','RESTORE_READY');

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_session_workspace_storage() RETURNS trigger
LANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog AS $$
DECLARE
    v_ack jsonb;
    v_join jsonb;
BEGIN
    IF TG_OP='DELETE' THEN
        IF NOT control_plane.project_purge_authorized() OR OLD.state<>'PURGED' THEN
            RAISE EXCEPTION 'session workspace metadata is retained';
        END IF;
        RETURN OLD;
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM control_plane.sessions session
        JOIN control_plane.agents agent ON agent.id=NEW.agent_id
            AND agent.organization_id=NEW.organization_id AND agent.project_id=NEW.project_id
        WHERE session.id=NEW.session_id AND session.organization_id=NEW.organization_id
            AND session.project_id=NEW.project_id AND session.target_type='AGENT' AND session.target_ref=agent.ref
    ) THEN
        RAISE EXCEPTION 'session workspace scope is invalid';
    END IF;
    IF TG_OP='INSERT' THEN
        IF NEW.generation<>1 OR NEW.state<>'EMPTY' OR NEW.version<>1 OR NEW.writer_lease_id IS NOT NULL OR
            NEW.source_archive_id IS NOT NULL OR NEW.current_archive_id IS NOT NULL THEN
            RAISE EXCEPTION 'session workspace initial state is invalid';
        END IF;
    ELSE
        IF ROW(NEW.id,NEW.ref,NEW.organization_id,NEW.project_id,NEW.session_id,NEW.agent_id,NEW.created_at)
            IS DISTINCT FROM ROW(OLD.id,OLD.ref,OLD.organization_id,OLD.project_id,OLD.session_id,OLD.agent_id,OLD.created_at)
            OR NEW.version<>OLD.version+1 OR OLD.state='PURGED' THEN
            RAISE EXCEPTION 'session workspace identity is immutable';
        END IF;
        IF NEW.generation<>OLD.generation THEN
            IF NEW.generation<>OLD.generation+1 OR OLD.state NOT IN ('CHECKPOINT_READY','RESTORED') OR
                NEW.state<>'RESTORE_READY' OR NEW.source_archive_id IS NULL OR
                NEW.source_archive_id IS DISTINCT FROM OLD.current_archive_id OR NEW.writer_lease_id IS NOT NULL OR
                NEW.writer_acknowledgement IS NOT NULL OR NEW.quiescence_record IS NOT NULL OR NEW.current_archive_id IS NOT NULL THEN
                RAISE EXCEPTION 'session workspace generation is not checkpointed';
            END IF;
        ELSIF NEW.state<>OLD.state AND NOT (
            (OLD.state IN ('EMPTY','RESTORED') AND NEW.state='WRITER_PENDING') OR
            (OLD.state='WRITER_PENDING' AND NEW.state='WRITING') OR
            (OLD.state='WRITING' AND NEW.state IN ('QUIESCING','CHECKPOINT_PENDING')) OR
            (OLD.state='QUIESCING' AND NEW.state='CHECKPOINT_PENDING') OR
            (OLD.state='CHECKPOINT_PENDING' AND NEW.state='CHECKPOINT_READY') OR
            (OLD.state='CHECKPOINT_READY' AND NEW.state='DELETE_READY') OR
            (OLD.state='DELETE_READY' AND NEW.state='ARCHIVED') OR
            (OLD.state='ARCHIVED' AND NEW.state='RESTORE_READY') OR
            (OLD.state='RESTORE_READY' AND NEW.state='RESTORING') OR
            (OLD.state='RESTORING' AND NEW.state='RESTORED') OR
            (OLD.state='BLOCKED' AND NEW.state='CHECKPOINT_PENDING' AND OLD.writer_acknowledgement IS NOT NULL) OR
            (NEW.state='BLOCKED') OR
            (NEW.state='PURGED' AND OLD.state IN ('CHECKPOINT_READY','ARCHIVED','RESTORED'))
        ) THEN
            RAISE EXCEPTION 'session workspace transition is invalid';
        END IF;
        IF NEW.generation=OLD.generation AND (
            (OLD.writer_acknowledgement IS NOT NULL AND NEW.writer_acknowledgement IS DISTINCT FROM OLD.writer_acknowledgement) OR
            (OLD.quiescence_record IS NOT NULL AND NEW.quiescence_record IS DISTINCT FROM OLD.quiescence_record) OR
            (OLD.writer_lease_id IS NOT NULL AND NEW.writer_lease_id IS DISTINCT FROM OLD.writer_lease_id) OR
            (NEW.source_archive_id IS DISTINCT FROM OLD.source_archive_id) OR
            (OLD.current_archive_id IS NOT NULL AND NEW.current_archive_id IS DISTINCT FROM OLD.current_archive_id)
        ) THEN
            RAISE EXCEPTION 'session workspace execution pins are immutable';
        END IF;
    END IF;
    IF NEW.writer_lease_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM control_plane.runtime_leases lease
        JOIN control_plane.runtime_revisions revision ON revision.id=lease.runtime_revision_id
            AND revision.organization_id=NEW.organization_id AND revision.session_id=NEW.session_id
            AND revision.project_id=NEW.project_id AND revision.agent_id=NEW.agent_id
        WHERE lease.id=NEW.writer_lease_id AND lease.organization_id=NEW.organization_id
    ) THEN
        RAISE EXCEPTION 'session workspace writer scope is invalid';
    END IF;
    IF NEW.writer_acknowledgement IS NOT NULL THEN
        v_ack:=NEW.writer_acknowledgement::jsonb;
        IF jsonb_typeof(v_ack)<>'object' OR (SELECT count(*) FROM jsonb_object_keys(v_ack))<>5 OR
            NOT (v_ack ?& ARRAY['schema','scope','workspace_ref','generation','writer']) OR
            v_ack->>'schema' IS DISTINCT FROM 'kodex.runtime-workspace-writer.v1' OR v_ack->>'workspace_ref' IS DISTINCT FROM NEW.ref OR
            v_ack->'generation' IS DISTINCT FROM to_jsonb(NEW.generation) OR jsonb_typeof(v_ack->'writer') IS DISTINCT FROM 'object' OR
            (SELECT count(*) FROM jsonb_object_keys(v_ack->'writer'))<>14 OR
            NOT ((v_ack->'writer') ?& ARRAY['run_ref','node_ref','turn_ref','runtime_revision_ref','runtime_revision_version',
                'runtime_revision_digest','execution_binding_digest','input_digest','lease_ref','lease_generation','attempt','pod_uid','pvc_uid','join_sha256']) OR
            v_ack->'writer'->>'join_sha256' IS DISTINCT FROM '' OR
            ((v_ack->'writer'->>'pod_uid') ~ '^[a-z0-9][a-z0-9-]{0,127}$') IS NOT TRUE OR
            ((v_ack->'writer'->>'pvc_uid') ~ '^[a-z0-9][a-z0-9-]{0,127}$') IS NOT TRUE OR
            ((v_ack->'writer'->>'execution_binding_digest') ~ '^[a-f0-9]{64}$') IS NOT TRUE OR
            NOT EXISTS (SELECT 1 FROM control_plane.runtime_leases lease
                JOIN control_plane.runtime_revisions revision ON revision.id=lease.runtime_revision_id
                JOIN control_plane.sessions session ON session.id=NEW.session_id
                JOIN control_plane.runs run ON run.id=lease.run_id AND run.session_id=NEW.session_id
                JOIN control_plane.run_nodes node ON node.id=lease.node_id AND node.agent_id=NEW.agent_id AND node.run_id=run.id
                JOIN control_plane.session_turns turn ON turn.id=revision.turn_id AND turn.session_id=NEW.session_id AND turn.run_id=run.id
                JOIN control_plane.agents agent ON agent.id=NEW.agent_id
                JOIN control_plane.projects project ON project.id=NEW.project_id
                JOIN control_plane.organizations organization ON organization.id=NEW.organization_id
                WHERE lease.id=NEW.writer_lease_id AND v_ack->'writer'->>'lease_ref'=lease.ref
                    AND v_ack->'writer'->'lease_generation'=to_jsonb(lease.generation)
                    AND v_ack->'writer'->>'runtime_revision_ref'=revision.ref
                    AND v_ack->'writer'->'runtime_revision_version'=to_jsonb(revision.generation)
                    AND v_ack->'writer'->>'runtime_revision_digest'=revision.revision_digest
                    AND v_ack->'writer'->>'input_digest'=lease.input_digest
                    AND v_ack->'writer'->>'run_ref'=run.ref AND v_ack->'writer'->>'node_ref'=node.ref
                    AND v_ack->'writer'->>'turn_ref'=turn.ref AND v_ack->'writer'->'attempt'=to_jsonb(revision.attempt)
                    AND v_ack->'scope'=jsonb_build_object('organization_ref',organization.ref,'project_ref',project.ref,'session_ref',session.ref,'agent_ref',agent.ref)) THEN
            RAISE EXCEPTION 'session workspace writer acknowledgement is invalid';
        END IF;
    END IF;
    IF NEW.quiescence_record IS NOT NULL THEN
        v_join:=NEW.quiescence_record::jsonb;
        IF v_ack IS NULL OR jsonb_typeof(v_join)<>'object' OR (SELECT count(*) FROM jsonb_object_keys(v_join))<>6 OR
            NOT (v_join ?& ARRAY['schema','scope','workspace_ref','generation','writer','joined']) OR
            v_join->>'schema' IS DISTINCT FROM 'kodex.runtime-workspace-quiescence.v1' OR v_join->'joined' IS DISTINCT FROM 'true'::jsonb OR
            (v_join-'schema'-'joined') IS DISTINCT FROM (v_ack-'schema') THEN
            RAISE EXCEPTION 'session workspace quiescence is invalid';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.guard_session_workspace_storage() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.guard_session_workspace_storage() TO control_plane_runtime;
CREATE TRIGGER session_workspace_storage_guard BEFORE INSERT OR UPDATE OR DELETE ON control_plane.session_workspace_storage
    FOR EACH ROW EXECUTE FUNCTION control_plane.guard_session_workspace_storage();
REVOKE ALL ON TABLE control_plane.session_workspace_storage FROM PUBLIC;
GRANT SELECT,INSERT,UPDATE,DELETE ON TABLE control_plane.session_workspace_storage TO control_plane_runtime;

RESET ROLE;

-- +goose Down
-- Work metadata и checkpoints не удаляются ради binary rollback.
SELECT 1;
