-- +goose Up
SET ROLE control_plane_owner;

ALTER TABLE control_plane.managed_configuration_sets
    DROP CONSTRAINT managed_configuration_sets_check,
    ADD CONSTRAINT managed_configuration_sets_source_owner CHECK (
        (managed_by='UI' AND source='control-center' AND source_revision='') OR
        (managed_by='GIT' AND source<>'control-center')),
    ADD CONSTRAINT managed_configuration_sets_source_tenant UNIQUE(id,organization_id);
ALTER TABLE control_plane.integration_connections
    ADD CONSTRAINT integration_connections_source_tenant UNIQUE(id,organization_id);
ALTER TABLE control_plane.integration_credential_revisions
    ADD CONSTRAINT integration_credentials_source_tenant UNIQUE(id,connection_id,organization_id);

CREATE TABLE control_plane.managed_configuration_git_sources (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^mcsrc_[A-Za-z0-9_-]{8,89}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    configuration_set_id uuid NOT NULL UNIQUE,
    root_actor_id uuid NOT NULL REFERENCES control_plane.subjects(id),
    connection_id uuid NOT NULL,
    provider_key text NOT NULL CHECK (provider_key IN ('github','gitlab')),
    repository_ref text NOT NULL CHECK (length(repository_ref) BETWEEN 1 AND 256),
    ref_name text NOT NULL CHECK (length(ref_name) BETWEEN 1 AND 256),
    path text NOT NULL CHECK (length(path) BETWEEN 1 AND 512),
    content_format text NOT NULL CHECK (content_format IN ('JSON','YAML','TOML')),
    version bigint NOT NULL DEFAULT 1 CHECK (version>0),
    generation bigint NOT NULL DEFAULT 1 CHECK (generation>0),
    state text NOT NULL CHECK (state IN ('QUEUED','CLAIMED','READY','SYNC_BLOCKED','DETACHED')),
    accepted_commit_sha text NOT NULL DEFAULT '' CHECK (accepted_commit_sha='' OR accepted_commit_sha ~ '^([0-9a-f]{40}|[0-9a-f]{64})$'),
    accepted_content_sha256 text NOT NULL DEFAULT '' CHECK (accepted_content_sha256='' OR accepted_content_sha256 ~ '^[0-9a-f]{64}$'),
    accepted_revision_id uuid,
    synced_at timestamptz,
    failure_code text NOT NULL DEFAULT '' CHECK (failure_code IN ('','UNAVAILABLE','CREDENTIAL_REJECTED','ACCESS_DENIED','NOT_FOUND','DIVERGED','CONTENT_INVALID','RESPONSE_INVALID')),
    next_refresh_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    FOREIGN KEY(configuration_set_id,organization_id) REFERENCES control_plane.managed_configuration_sets(id,organization_id),
    FOREIGN KEY(connection_id,organization_id) REFERENCES control_plane.integration_connections(id,organization_id),
    FOREIGN KEY(configuration_set_id,accepted_revision_id) REFERENCES control_plane.managed_configuration_revisions(configuration_set_id,id),
    UNIQUE(id,organization_id),
    CHECK ((accepted_revision_id IS NULL AND accepted_commit_sha='' AND accepted_content_sha256='' AND synced_at IS NULL) OR
           (accepted_revision_id IS NOT NULL AND accepted_commit_sha<>'' AND accepted_content_sha256<>'' AND synced_at IS NOT NULL))
);

CREATE TABLE control_plane.managed_configuration_source_work (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^mcswork_[A-Za-z0-9_-]{8,88}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    source_id uuid NOT NULL,
    source_generation bigint NOT NULL CHECK (source_generation>0),
    root_actor_id uuid NOT NULL REFERENCES control_plane.subjects(id),
    connection_id uuid NOT NULL,
    connection_version bigint NOT NULL CHECK (connection_version>0),
    credential_revision_id uuid NOT NULL,
    input_snapshot jsonb NOT NULL CHECK (jsonb_typeof(input_snapshot)='object' AND octet_length(input_snapshot::text)<=262144),
    input_sha256 text NOT NULL CHECK (input_sha256 ~ '^[0-9a-f]{64}$'),
    state text NOT NULL CHECK (state IN ('QUEUED','CLAIMED','COMPLETED','FAILED','CANCELLED','EXPIRED')),
    attempt bigint NOT NULL DEFAULT 1 CHECK (attempt BETWEEN 1 AND 3),
    claim_generation bigint NOT NULL DEFAULT 0 CHECK (claim_generation>=0),
    claimant text NOT NULL DEFAULT '' CHECK (length(claimant)<=128),
    fence text NOT NULL DEFAULT '' CHECK (length(fence)<=128),
    lease_expires_at timestamptz,
    deadline timestamptz NOT NULL DEFAULT clock_timestamp()+interval '15 minutes',
    completion_sha256 text NOT NULL DEFAULT '' CHECK (completion_sha256='' OR completion_sha256 ~ '^[0-9a-f]{64}$'),
    receipt jsonb CHECK (receipt IS NULL OR jsonb_typeof(receipt)='object' AND octet_length(receipt::text)<=16384),
    failure_code text NOT NULL DEFAULT '' CHECK (failure_code IN ('','UNAVAILABLE','CREDENTIAL_REJECTED','ACCESS_DENIED','NOT_FOUND','DIVERGED','CONTENT_INVALID','RESPONSE_INVALID')),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    FOREIGN KEY(source_id,organization_id) REFERENCES control_plane.managed_configuration_git_sources(id,organization_id),
    FOREIGN KEY(connection_id,organization_id) REFERENCES control_plane.integration_connections(id,organization_id),
    FOREIGN KEY(credential_revision_id,connection_id,organization_id) REFERENCES control_plane.integration_credential_revisions(id,connection_id,organization_id),
    CHECK (state<>'CLAIMED' OR (claim_generation>0 AND claimant<>'' AND fence<>'' AND lease_expires_at IS NOT NULL)),
    CHECK (lease_expires_at IS NULL OR lease_expires_at<=deadline)
);
CREATE UNIQUE INDEX managed_configuration_source_one_active_work
    ON control_plane.managed_configuration_source_work(source_id) WHERE state IN ('QUEUED','CLAIMED');
CREATE INDEX managed_configuration_source_work_due
    ON control_plane.managed_configuration_source_work(created_at,id) WHERE state IN ('QUEUED','CLAIMED');
CREATE INDEX managed_configuration_source_refresh_due
    ON control_plane.managed_configuration_git_sources(next_refresh_at,id) WHERE state IN ('READY','SYNC_BLOCKED');

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_configuration_source_work() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='DELETE' THEN
        RAISE EXCEPTION 'configuration source work cannot be deleted';
    END IF;
    IF OLD.state IN ('COMPLETED','FAILED','CANCELLED','EXPIRED') OR
       ROW(NEW.id,NEW.ref,NEW.organization_id,NEW.source_id,NEW.source_generation,NEW.root_actor_id,
           NEW.connection_id,NEW.connection_version,NEW.credential_revision_id,NEW.input_snapshot,NEW.input_sha256,NEW.deadline,NEW.created_at)
       IS DISTINCT FROM
       ROW(OLD.id,OLD.ref,OLD.organization_id,OLD.source_id,OLD.source_generation,OLD.root_actor_id,
           OLD.connection_id,OLD.connection_version,OLD.credential_revision_id,OLD.input_snapshot,OLD.input_sha256,OLD.deadline,OLD.created_at) OR
       NEW.claim_generation<OLD.claim_generation OR NEW.attempt<OLD.attempt THEN
        RAISE EXCEPTION 'configuration source work input or terminal state is immutable';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER configuration_source_work_guard BEFORE UPDATE OR DELETE
    ON control_plane.managed_configuration_source_work FOR EACH ROW
    EXECUTE FUNCTION control_plane.guard_configuration_source_work();

GRANT SELECT,INSERT,UPDATE ON control_plane.managed_configuration_git_sources,control_plane.managed_configuration_source_work TO control_plane_runtime;
RESET ROLE;
