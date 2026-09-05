-- +goose Up
SET ROLE control_plane_owner;

CREATE TABLE control_plane.memory_records (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^memr_[A-Za-z0-9_-]{8,90}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    project_id uuid NOT NULL REFERENCES control_plane.projects(id),
    agent_id uuid REFERENCES control_plane.agents(id),
    state text NOT NULL DEFAULT 'ACTIVE' CHECK (state IN ('ACTIVE','ARCHIVED','PURGED')),
    version bigint NOT NULL DEFAULT 1 CHECK (version>0),
    current_revision_id uuid,
    created_by uuid NOT NULL REFERENCES control_plane.subjects(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE control_plane.memory_record_revisions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^memv_[A-Za-z0-9_-]{8,90}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    record_id uuid NOT NULL REFERENCES control_plane.memory_records(id),
    revision bigint NOT NULL CHECK (revision>0),
    title text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 160),
    summary text NOT NULL CHECK (octet_length(summary)<=65536),
    digest text NOT NULL CHECK (digest ~ '^[a-f0-9]{64}$'),
    parent_revision_id uuid REFERENCES control_plane.memory_record_revisions(id),
    source_run_id uuid REFERENCES control_plane.runs(id),
    source_run_version bigint,
    source_kind text NOT NULL CHECK (source_kind IN ('USER_SUMMARY','RUN_SUMMARY')),
    retention_until timestamptz NOT NULL,
    created_by uuid NOT NULL REFERENCES control_plane.subjects(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE(record_id,revision), UNIQUE(record_id,id),
    FOREIGN KEY(record_id,parent_revision_id) REFERENCES control_plane.memory_record_revisions(record_id,id),
    CHECK ((source_run_id IS NULL)=(source_run_version IS NULL)),
    CHECK (source_run_version IS NULL OR source_run_version>0),
    CHECK (retention_until>created_at)
);
ALTER TABLE control_plane.memory_records ADD CONSTRAINT memory_record_current_revision_fk
    FOREIGN KEY(id,current_revision_id) REFERENCES control_plane.memory_record_revisions(record_id,id)
    DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE control_plane.skill_bundles (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^sklb_[A-Za-z0-9_-]{8,90}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    project_id uuid NOT NULL REFERENCES control_plane.projects(id),
    state text NOT NULL DEFAULT 'ACTIVE' CHECK (state IN ('ACTIVE','ARCHIVED','PURGED')),
    version bigint NOT NULL DEFAULT 1 CHECK (version>0),
    current_revision_id uuid,
    draft_revision_id uuid,
    created_by uuid NOT NULL REFERENCES control_plane.subjects(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE control_plane.skill_bundle_revisions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^sklv_[A-Za-z0-9_-]{8,90}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    bundle_id uuid NOT NULL REFERENCES control_plane.skill_bundles(id),
    revision bigint NOT NULL CHECK (revision>0),
    state text NOT NULL CHECK (state IN ('DRAFT','INVALID','VALIDATED','APPROVED','REJECTED','PUBLISHED','DISCARDED')),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 160),
    description text NOT NULL DEFAULT '' CHECK (char_length(description)<=2000),
    files jsonb NOT NULL CHECK (jsonb_typeof(files)='array' AND jsonb_array_length(files)<=128),
    digest text NOT NULL CHECK (digest ~ '^[a-f0-9]{64}$'),
    parent_revision_id uuid REFERENCES control_plane.skill_bundle_revisions(id),
    scan_state text NOT NULL DEFAULT 'PENDING' CHECK (scan_state IN ('PENDING','CLEAN','INFECTED','ERROR')),
    scan_engine text NOT NULL DEFAULT '',
    scan_digest text NOT NULL DEFAULT '' CHECK (scan_digest='' OR scan_digest ~ '^[a-f0-9]{64}$'),
    scanned_at timestamptz,
    reviewed_by uuid REFERENCES control_plane.subjects(id),
    reviewed_at timestamptz,
    review_comment text NOT NULL DEFAULT '' CHECK (char_length(review_comment)<=2000),
    diagnostics jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(diagnostics)='array'),
    created_by uuid NOT NULL REFERENCES control_plane.subjects(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE(bundle_id,revision), UNIQUE(bundle_id,id),
    CHECK (scan_state<>'CLEAN' OR (scan_engine<>'' AND scan_digest<>'' AND scanned_at IS NOT NULL)),
    CHECK (state NOT IN ('APPROVED','PUBLISHED') OR (scan_state='CLEAN' AND reviewed_by IS NOT NULL AND reviewed_at IS NOT NULL))
);
ALTER TABLE control_plane.skill_bundles ADD CONSTRAINT skill_bundle_current_revision_fk
    FOREIGN KEY(id,current_revision_id) REFERENCES control_plane.skill_bundle_revisions(bundle_id,id)
    DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE control_plane.skill_bundles ADD CONSTRAINT skill_bundle_draft_revision_fk
    FOREIGN KEY(id,draft_revision_id) REFERENCES control_plane.skill_bundle_revisions(bundle_id,id)
    DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE control_plane.agent_context_bindings (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^ctxb_[A-Za-z0-9_-]{8,90}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    project_id uuid NOT NULL REFERENCES control_plane.projects(id),
    agent_id uuid NOT NULL REFERENCES control_plane.agents(id),
    memory_record_id uuid REFERENCES control_plane.memory_records(id),
    memory_revision_id uuid,
    skill_bundle_id uuid REFERENCES control_plane.skill_bundles(id),
    skill_revision_id uuid,
    version bigint NOT NULL DEFAULT 1 CHECK (version>0),
    enabled boolean NOT NULL DEFAULT true,
    created_by uuid NOT NULL REFERENCES control_plane.subjects(id),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    FOREIGN KEY(memory_record_id,memory_revision_id) REFERENCES control_plane.memory_record_revisions(record_id,id),
    FOREIGN KEY(skill_bundle_id,skill_revision_id) REFERENCES control_plane.skill_bundle_revisions(bundle_id,id),
    CHECK ((memory_record_id IS NOT NULL AND memory_revision_id IS NOT NULL AND skill_bundle_id IS NULL AND skill_revision_id IS NULL)
        OR (skill_bundle_id IS NOT NULL AND skill_revision_id IS NOT NULL AND memory_record_id IS NULL AND memory_revision_id IS NULL)),
    UNIQUE(agent_id,memory_record_id), UNIQUE(agent_id,skill_bundle_id)
);
CREATE INDEX memory_records_catalog ON control_plane.memory_records(organization_id,project_id,ref);
CREATE INDEX memory_records_agent ON control_plane.memory_records(agent_id,ref);
CREATE INDEX memory_revision_retention ON control_plane.memory_record_revisions(retention_until,record_id);
-- +goose StatementBegin
CREATE FUNCTION control_plane.memory_revision_projection(revision_id uuid)
RETURNS jsonb LANGUAGE sql STABLE SECURITY INVOKER AS $$
    SELECT jsonb_build_object('ref',revision.ref,'revision',revision.revision,'title',revision.title,
        'summary',CASE WHEN record.state='PURGED' OR revision.retention_until<=statement_timestamp() THEN '' ELSE revision.summary END,
        'digest',revision.digest,'parentRevisionRef',COALESCE(parent.ref,''),
        'retentionUntil',revision.retention_until,'redacted',record.state='PURGED' OR revision.retention_until<=statement_timestamp(),
        'provenance',jsonb_build_object('actorRef',actor.ref,'sourceKind',revision.source_kind,'sourceRef',COALESCE(run.ref,''),
            'sourceRevision',COALESCE(revision.source_run_version::text,''),'digest',revision.digest,'createdAt',revision.created_at))
    FROM control_plane.memory_record_revisions revision
    JOIN control_plane.memory_records record ON record.id=revision.record_id
    JOIN control_plane.subjects actor ON actor.id=revision.created_by
    LEFT JOIN control_plane.memory_record_revisions parent ON parent.id=revision.parent_revision_id
    LEFT JOIN control_plane.runs run ON run.id=revision.source_run_id
    WHERE revision.id=revision_id;
$$;
-- +goose StatementEnd
CREATE VIEW control_plane.memory_record_projection WITH (security_invoker=true) AS
SELECT record.id,record.organization_id,record.project_id,record.agent_id,record.current_revision_id,
    record.ref,record.version,record.state AS stored_state,record.created_by,project.ref AS project_ref,
    COALESCE(agent.ref,'') AS agent_ref,revision.source_run_id,revision.title,
    CASE WHEN record.state='ACTIVE' AND revision.retention_until<=statement_timestamp() THEN 'EXPIRED' ELSE record.state END AS state,
    jsonb_build_object('ref',record.ref,'version',record.version,'projectRef',project.ref,'agentRef',COALESCE(agent.ref,''),
        'state',CASE WHEN record.state='ACTIVE' AND revision.retention_until<=statement_timestamp() THEN 'EXPIRED' ELSE record.state END,
        'currentRevision',control_plane.memory_revision_projection(record.current_revision_id),'createdAt',record.created_at,'updatedAt',record.updated_at) AS projection
FROM control_plane.memory_records record
JOIN control_plane.projects project ON project.id=record.project_id AND project.organization_id=record.organization_id
LEFT JOIN control_plane.agents agent ON agent.id=record.agent_id AND agent.project_id=record.project_id
LEFT JOIN control_plane.memory_record_revisions revision ON revision.id=record.current_revision_id;
-- +goose StatementBegin
CREATE FUNCTION control_plane.memory_record_visible(tenant uuid,actor uuid,record_id uuid,evaluated_at timestamptz)
RETURNS boolean LANGUAGE sql STABLE SECURITY INVOKER AS $$
    SELECT EXISTS (
        SELECT 1 FROM control_plane.memory_records record
        JOIN control_plane.catalog_access_targets target ON target.organization_id=record.organization_id
          AND target.kind=CASE WHEN record.agent_id IS NULL THEN 'PROJECT' ELSE 'AGENT' END
          AND target.id=COALESCE(record.agent_id,record.project_id)
        WHERE record.organization_id=tenant AND record.id=record_id
          AND control_plane.catalog_resource_visible(tenant,actor,
            CASE WHEN record.agent_id IS NULL THEN 'project.view' ELSE 'agent.view' END,
            target.kind,target.id,target.project_id,target.owner_id,target.related_ids,evaluated_at,false)
    );
$$;
-- +goose StatementEnd
CREATE INDEX skill_bundles_catalog ON control_plane.skill_bundles(organization_id,project_id,ref);
-- +goose StatementBegin
CREATE FUNCTION control_plane.protect_memory_record_revision()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='DELETE' THEN RAISE EXCEPTION 'memory revision cannot be deleted'; END IF;
    IF to_jsonb(NEW)-'summary' IS DISTINCT FROM to_jsonb(OLD)-'summary'
       OR NEW.summary<>'' OR NOT EXISTS (SELECT 1 FROM control_plane.memory_records WHERE id=OLD.record_id AND state='PURGED') THEN
        RAISE EXCEPTION 'memory revision is immutable';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER protect_memory_record_revision BEFORE UPDATE OR DELETE ON control_plane.memory_record_revisions
FOR EACH ROW EXECUTE FUNCTION control_plane.protect_memory_record_revision();
-- +goose StatementBegin
CREATE FUNCTION control_plane.protect_memory_record()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='DELETE' THEN RAISE EXCEPTION 'memory tombstone cannot be deleted'; END IF;
    IF ROW(NEW.id,NEW.ref,NEW.organization_id,NEW.project_id,NEW.agent_id,NEW.created_by,NEW.created_at)
        IS DISTINCT FROM ROW(OLD.id,OLD.ref,OLD.organization_id,OLD.project_id,OLD.agent_id,OLD.created_by,OLD.created_at) THEN
        RAISE EXCEPTION 'memory identity is immutable';
    END IF;
    IF OLD.state='PURGED' AND NEW IS DISTINCT FROM OLD THEN RAISE EXCEPTION 'purged memory is terminal'; END IF;
    IF NEW.state IS DISTINCT FROM OLD.state AND NOT (
        (OLD.state='ACTIVE' AND NEW.state='ARCHIVED') OR
        (OLD.state='ARCHIVED' AND NEW.state IN ('ACTIVE','PURGED'))) THEN
        RAISE EXCEPTION 'memory state transition is invalid';
    END IF;
    IF NEW.version<>OLD.version+1 AND NOT (OLD.current_revision_id IS NULL AND NEW.current_revision_id IS NOT NULL AND NEW.version=OLD.version) THEN
        RAISE EXCEPTION 'memory version must advance';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER protect_memory_record BEFORE UPDATE OR DELETE ON control_plane.memory_records
FOR EACH ROW EXECUTE FUNCTION control_plane.protect_memory_record();
-- +goose StatementBegin
CREATE FUNCTION control_plane.protect_skill_bundle_revision()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='DELETE' THEN RAISE EXCEPTION 'skill revision cannot be deleted'; END IF;
    IF ROW(NEW.id,NEW.ref,NEW.organization_id,NEW.bundle_id,NEW.revision,NEW.parent_revision_id,NEW.created_by,NEW.created_at)
        IS DISTINCT FROM ROW(OLD.id,OLD.ref,OLD.organization_id,OLD.bundle_id,OLD.revision,OLD.parent_revision_id,OLD.created_by,OLD.created_at) THEN
        RAISE EXCEPTION 'skill revision identity is immutable';
    END IF;
    IF OLD.state IN ('PUBLISHED','DISCARDED') AND NEW IS DISTINCT FROM OLD THEN
        IF to_jsonb(NEW)-'files' IS DISTINCT FROM to_jsonb(OLD)-'files' OR NEW.files<>'[]'::jsonb
            OR NOT EXISTS (SELECT 1 FROM control_plane.skill_bundles WHERE id=OLD.bundle_id AND state='PURGED') THEN
            RAISE EXCEPTION 'terminal skill revision is immutable';
        END IF;
    END IF;
    IF ROW(NEW.name,NEW.description,NEW.files,NEW.digest) IS DISTINCT FROM ROW(OLD.name,OLD.description,OLD.files,OLD.digest)
       AND OLD.state NOT IN ('PUBLISHED','DISCARDED')
       AND (NEW.state<>'DRAFT' OR NEW.scan_state<>'PENDING' OR NEW.reviewed_by IS NOT NULL OR NEW.reviewed_at IS NOT NULL) THEN
        RAISE EXCEPTION 'skill edit must invalidate validation and review';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER protect_skill_bundle_revision BEFORE UPDATE OR DELETE ON control_plane.skill_bundle_revisions
FOR EACH ROW EXECUTE FUNCTION control_plane.protect_skill_bundle_revision();
GRANT SELECT,INSERT,UPDATE ON control_plane.memory_records,control_plane.memory_record_revisions,
    control_plane.skill_bundles,control_plane.skill_bundle_revisions,control_plane.agent_context_bindings TO control_plane_runtime;
GRANT SELECT ON control_plane.memory_record_projection TO control_plane_runtime;
REVOKE ALL ON FUNCTION control_plane.memory_revision_projection(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.memory_revision_projection(uuid) TO control_plane_runtime;
REVOKE ALL ON FUNCTION control_plane.memory_record_visible(uuid,uuid,uuid,timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.memory_record_visible(uuid,uuid,uuid,timestamptz) TO control_plane_runtime;
RESET ROLE;
