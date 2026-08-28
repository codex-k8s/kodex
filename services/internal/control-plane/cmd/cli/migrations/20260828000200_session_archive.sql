-- +goose Up
SET ROLE control_plane_owner;

CREATE TABLE control_plane.session_archives (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^sar_[A-Za-z0-9_-]{8,89}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    project_id uuid REFERENCES control_plane.projects(id),
    session_id uuid NOT NULL REFERENCES control_plane.sessions(id),
    provider_account_id uuid NOT NULL REFERENCES control_plane.provider_accounts(id),
    runtime_revision_id uuid NOT NULL REFERENCES control_plane.runtime_revisions(id),
    codex_session_id uuid NOT NULL,
    content_generation bigint NOT NULL CHECK (content_generation > 0),
    format_version integer NOT NULL CHECK (format_version = 1),
    source_relative_path text NOT NULL CHECK (
        source_relative_path ~ '^\.kodex/state/codex-home/sessions/[0-9]{4}/[0-9]{2}/[0-9]{2}/rollout-[0-9a-f-]{36}\.jsonl$'
        AND source_relative_path !~ '(^|/)\.\.(/|$)'
    ),
    source_sha256 text NOT NULL CHECK (source_sha256 ~ '^[a-f0-9]{64}$'),
    source_size_bytes bigint NOT NULL CHECK (source_size_bytes BETWEEN 1 AND 67108864),
    object_key text NOT NULL UNIQUE CHECK (char_length(object_key) BETWEEN 1 AND 1024),
    object_version text NOT NULL CHECK (char_length(object_version) <= 1024),
    object_etag text NOT NULL CHECK (char_length(object_etag) BETWEEN 1 AND 256),
    object_digest text NOT NULL CHECK (object_digest ~ '^sha256:[a-f0-9]{64}$'),
    object_size_bytes bigint NOT NULL CHECK (object_size_bytes BETWEEN 1 AND 71303168),
    lifecycle_state text NOT NULL DEFAULT 'AVAILABLE' CHECK (lifecycle_state IN ('AVAILABLE', 'SUPERSEDED', 'DELETED')),
    retention_until timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    deleted_at timestamptz,
    UNIQUE (session_id, content_generation),
    CHECK ((lifecycle_state = 'DELETED') = (deleted_at IS NOT NULL))
);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.protect_session_archive_receipt()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'session archive receipt is immutable';
    END IF;
    IF ROW(NEW.id, NEW.ref, NEW.organization_id, NEW.project_id, NEW.session_id,
           NEW.provider_account_id, NEW.runtime_revision_id, NEW.codex_session_id,
           NEW.content_generation, NEW.format_version, NEW.source_relative_path,
           NEW.source_sha256, NEW.source_size_bytes, NEW.object_key, NEW.object_version,
           NEW.object_etag, NEW.object_digest, NEW.object_size_bytes, NEW.created_at)
       IS DISTINCT FROM
       ROW(OLD.id, OLD.ref, OLD.organization_id, OLD.project_id, OLD.session_id,
           OLD.provider_account_id, OLD.runtime_revision_id, OLD.codex_session_id,
           OLD.content_generation, OLD.format_version, OLD.source_relative_path,
           OLD.source_sha256, OLD.source_size_bytes, OLD.object_key, OLD.object_version,
           OLD.object_etag, OLD.object_digest, OLD.object_size_bytes, OLD.created_at) THEN
        RAISE EXCEPTION 'session archive receipt identity is immutable';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER protect_session_archive_receipt
BEFORE UPDATE OR DELETE ON control_plane.session_archives
FOR EACH ROW EXECUTE FUNCTION control_plane.protect_session_archive_receipt();

CREATE TABLE control_plane.session_storage (
    session_id uuid PRIMARY KEY REFERENCES control_plane.sessions(id) ON DELETE CASCADE,
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    project_id uuid REFERENCES control_plane.projects(id),
    provider_account_id uuid NOT NULL REFERENCES control_plane.provider_accounts(id),
    runtime_revision_id uuid NOT NULL REFERENCES control_plane.runtime_revisions(id),
    codex_session_id uuid NOT NULL,
    content_generation bigint NOT NULL CHECK (content_generation > 0),
    state text NOT NULL CHECK (state IN (
        'LIVE', 'SNAPSHOT_READY', 'SNAPSHOTTING', 'DELETE_PVC_READY',
        'ARCHIVED', 'RESTORE_READY', 'RESTORING', 'ERROR', 'PURGED'
    )),
    source_relative_path text NOT NULL CHECK (
        source_relative_path ~ '^\.kodex/state/codex-home/sessions/[0-9]{4}/[0-9]{2}/[0-9]{2}/rollout-[0-9a-f-]{36}\.jsonl$'
        AND source_relative_path !~ '(^|/)\.\.(/|$)'
    ),
    source_sha256 text NOT NULL CHECK (source_sha256 ~ '^[a-f0-9]{64}$'),
    source_size_bytes bigint NOT NULL CHECK (source_size_bytes BETWEEN 1 AND 67108864),
    current_archive_id uuid REFERENCES control_plane.session_archives(id),
    idle_since timestamptz NOT NULL DEFAULT clock_timestamp(),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK (state NOT IN ('DELETE_PVC_READY', 'ARCHIVED', 'RESTORE_READY', 'RESTORING', 'PURGED') OR current_archive_id IS NOT NULL),
    CHECK (current_archive_id IS NULL OR state IN ('DELETE_PVC_READY', 'ARCHIVED', 'RESTORE_READY', 'RESTORING', 'ERROR', 'PURGED'))
);

CREATE INDEX session_storage_snapshot_candidates
    ON control_plane.session_storage (idle_since, updated_at)
    WHERE state IN ('LIVE', 'SNAPSHOT_READY');

CREATE TABLE control_plane.session_archive_tasks (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^sat_[A-Za-z0-9_-]{8,89}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    project_id uuid REFERENCES control_plane.projects(id),
    session_id uuid NOT NULL REFERENCES control_plane.sessions(id),
    archive_id uuid REFERENCES control_plane.session_archives(id),
    kind text NOT NULL CHECK (kind IN ('SNAPSHOT', 'RESTORE', 'DELETE_PVC', 'DELETE_OBJECT')),
    state text NOT NULL CHECK (state IN ('READY', 'CLAIMED', 'SUCCEEDED', 'CANCELLED', 'DEAD_LETTER')),
    content_generation bigint NOT NULL CHECK (content_generation > 0),
    input_digest text NOT NULL CHECK (input_digest ~ '^[a-f0-9]{64}$'),
    object_key text CHECK (object_key IS NULL OR char_length(object_key) BETWEEN 1 AND 1024),
    object_version text CHECK (object_version IS NULL OR char_length(object_version) <= 1024),
    attempt integer NOT NULL DEFAULT 0 CHECK (attempt BETWEEN 0 AND 10),
    maximum_attempts integer NOT NULL DEFAULT 5 CHECK (maximum_attempts BETWEEN 1 AND 10),
    generation bigint NOT NULL DEFAULT 0 CHECK (generation >= 0),
    workload_instance text,
    lease_ref text UNIQUE CHECK (lease_ref IS NULL OR lease_ref ~ '^lea_[A-Za-z0-9_-]{8,89}$'),
    fence_digest text CHECK (fence_digest IS NULL OR fence_digest ~ '^[a-f0-9]{64}$'),
    lease_expires_at timestamptz,
    available_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    safe_error_code text NOT NULL DEFAULT '' CHECK (char_length(safe_error_code) <= 128),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    completed_at timestamptz,
    CHECK (
        (state = 'CLAIMED' AND workload_instance IS NOT NULL AND lease_ref IS NOT NULL AND fence_digest IS NOT NULL AND lease_expires_at IS NOT NULL)
        OR
        (state <> 'CLAIMED' AND workload_instance IS NULL AND lease_ref IS NULL AND fence_digest IS NULL AND lease_expires_at IS NULL)
    ),
    CHECK ((kind IN ('SNAPSHOT', 'DELETE_OBJECT')) = (object_key IS NOT NULL))
);

CREATE INDEX session_archive_tasks_claimable
    ON control_plane.session_archive_tasks (available_at, created_at)
    WHERE state IN ('READY', 'CLAIMED');
CREATE UNIQUE INDEX session_archive_tasks_open_session_kind
    ON control_plane.session_archive_tasks (session_id, kind)
    WHERE state IN ('READY', 'CLAIMED') AND kind <> 'DELETE_OBJECT';
CREATE UNIQUE INDEX session_archive_tasks_open_object_delete
    ON control_plane.session_archive_tasks (object_key)
    WHERE state IN ('READY', 'CLAIMED') AND kind = 'DELETE_OBJECT';
CREATE INDEX session_archives_gc_candidates
    ON control_plane.session_archives (retention_until, created_at)
    WHERE lifecycle_state IN ('AVAILABLE', 'SUPERSEDED');

RESET ROLE;

-- +goose Down
SET ROLE control_plane_owner;
DROP TABLE control_plane.session_archive_tasks;
DROP TABLE control_plane.session_storage;
DROP TRIGGER protect_session_archive_receipt ON control_plane.session_archives;
DROP FUNCTION control_plane.protect_session_archive_receipt();
DROP TABLE control_plane.session_archives;
RESET ROLE;
