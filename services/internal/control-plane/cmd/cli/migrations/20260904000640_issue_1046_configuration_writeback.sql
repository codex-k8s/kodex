-- +goose Up
SET ROLE control_plane_owner;

ALTER TABLE control_plane.managed_configuration_git_sources
    ADD COLUMN accepted_raw_content text CHECK (accepted_raw_content IS NULL OR octet_length(accepted_raw_content) BETWEEN 1 AND 262144),
    ADD CONSTRAINT configuration_source_raw_digest CHECK (accepted_raw_content IS NULL OR
      encode(digest(convert_to(accepted_raw_content,'UTF8'),'sha256'),'hex')=accepted_content_sha256);

CREATE TABLE control_plane.managed_configuration_writebacks (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^mcwb_[A-Za-z0-9_-]{8,89}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    configuration_set_id uuid NOT NULL,
    source_id uuid NOT NULL,
    root_actor_id uuid NOT NULL REFERENCES control_plane.subjects(id),
    approver_id uuid REFERENCES control_plane.subjects(id),
    connection_id uuid NOT NULL,
    credential_revision_id uuid NOT NULL,
    input_snapshot jsonb NOT NULL CHECK (jsonb_typeof(input_snapshot)='object' AND octet_length(input_snapshot::text)<=2097152),
    input_sha256 text NOT NULL CHECK (input_sha256 ~ '^[0-9a-f]{64}$'),
    approval_digest text NOT NULL CHECK (approval_digest ~ '^[0-9a-f]{64}$'),
    version bigint NOT NULL DEFAULT 1 CHECK (version>0),
    state text NOT NULL CHECK (state IN ('WAITING_APPROVAL','QUEUED','CLAIMED','EFFECT_STARTED','SUCCEEDED','REJECTED','CANCELLED','EXPIRED','FAILED','UNKNOWN_OUTCOME')),
    effect text NOT NULL DEFAULT 'BRANCH' CHECK (effect IN ('BRANCH','PULL_REQUEST')),
    attempt bigint NOT NULL DEFAULT 0 CHECK (attempt>=0),
    claim_generation bigint NOT NULL DEFAULT 0 CHECK (claim_generation>=0),
    claimant text NOT NULL DEFAULT '' CHECK (length(claimant)<=128),
    fence text NOT NULL DEFAULT '' CHECK (length(fence)<=128),
    lease_expires_at timestamptz,
    candidate_commit_sha text NOT NULL DEFAULT '' CHECK (candidate_commit_sha='' OR candidate_commit_sha ~ '^([0-9a-f]{40}|[0-9a-f]{64})$'),
    candidate_tree_sha text NOT NULL DEFAULT '' CHECK (candidate_tree_sha='' OR candidate_tree_sha ~ '^([0-9a-f]{40}|[0-9a-f]{64})$'),
    candidate_blob_sha text NOT NULL DEFAULT '' CHECK (candidate_blob_sha='' OR candidate_blob_sha ~ '^([0-9a-f]{40}|[0-9a-f]{64})$'),
    effect_started_at timestamptz,
    branch_confirmed_at timestamptz,
    pull_request_confirmed_at timestamptz,
    pull_request_ref text NOT NULL DEFAULT '' CHECK (length(pull_request_ref)<=128),
    pull_request_url text NOT NULL DEFAULT '' CHECK (length(pull_request_url)<=2048),
    failure_code text NOT NULL DEFAULT '' CHECK (failure_code IN ('','UNAVAILABLE','CREDENTIAL_REJECTED','ACCESS_DENIED','SOURCE_CHANGED','CONTENT_INVALID','RESPONSE_INVALID','AUTHORITY_CHANGED','DEADLINE_EXCEEDED','BRANCH_CONFLICT','OUTCOME_UNCONFIRMED')),
    receipts jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(receipts)='object' AND octet_length(receipts::text)<=262144),
    approved_at timestamptz,
    deadline timestamptz,
    expires_at timestamptz NOT NULL DEFAULT clock_timestamp()+interval '24 hours',
    completed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    FOREIGN KEY(configuration_set_id,organization_id) REFERENCES control_plane.managed_configuration_sets(id,organization_id),
    FOREIGN KEY(source_id,organization_id) REFERENCES control_plane.managed_configuration_git_sources(id,organization_id),
    FOREIGN KEY(connection_id,organization_id) REFERENCES control_plane.integration_connections(id,organization_id),
    FOREIGN KEY(credential_revision_id,connection_id,organization_id) REFERENCES control_plane.integration_credential_revisions(id,connection_id,organization_id),
    CHECK ((approved_at IS NULL AND approver_id IS NULL AND deadline IS NULL) OR (approved_at IS NOT NULL AND approver_id IS NOT NULL AND deadline IS NOT NULL)),
    CHECK (state NOT IN ('QUEUED','CLAIMED','EFFECT_STARTED','UNKNOWN_OUTCOME','SUCCEEDED') OR approved_at IS NOT NULL),
    CHECK (state NOT IN ('CLAIMED','EFFECT_STARTED') OR (claim_generation>0 AND claimant<>'' AND fence<>'' AND lease_expires_at IS NOT NULL)),
    CHECK (effect_started_at IS NULL OR candidate_commit_sha<>'' AND candidate_tree_sha<>'' AND candidate_blob_sha<>''),
    CHECK (state NOT IN ('EFFECT_STARTED','UNKNOWN_OUTCOME') OR effect_started_at IS NOT NULL),
    CHECK (state<>'SUCCEEDED' OR branch_confirmed_at IS NOT NULL AND pull_request_confirmed_at IS NOT NULL AND pull_request_ref<>'' AND pull_request_url<>''),
    CHECK (pull_request_confirmed_at IS NULL OR branch_confirmed_at IS NOT NULL)
);
CREATE UNIQUE INDEX managed_configuration_writeback_one_active ON control_plane.managed_configuration_writebacks(configuration_set_id)
    WHERE state IN ('WAITING_APPROVAL','QUEUED','CLAIMED','EFFECT_STARTED','UNKNOWN_OUTCOME');
CREATE INDEX managed_configuration_writeback_due ON control_plane.managed_configuration_writebacks(organization_id,updated_at,id)
    WHERE state IN ('WAITING_APPROVAL','QUEUED','CLAIMED','EFFECT_STARTED','UNKNOWN_OUTCOME');

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_configuration_writeback() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='DELETE' THEN
        RAISE EXCEPTION 'configuration writeback tombstone cannot be deleted';
    END IF;
    IF OLD.state IN ('SUCCEEDED','REJECTED','CANCELLED','EXPIRED','FAILED') OR
       ROW(NEW.id,NEW.ref,NEW.organization_id,NEW.configuration_set_id,NEW.source_id,NEW.root_actor_id,NEW.connection_id,
           NEW.credential_revision_id,NEW.input_snapshot,NEW.input_sha256,NEW.approval_digest,NEW.created_at,NEW.expires_at)
       IS DISTINCT FROM
       ROW(OLD.id,OLD.ref,OLD.organization_id,OLD.configuration_set_id,OLD.source_id,OLD.root_actor_id,OLD.connection_id,
           OLD.credential_revision_id,OLD.input_snapshot,OLD.input_sha256,OLD.approval_digest,OLD.created_at,OLD.expires_at) OR
       NEW.version<>OLD.version+1 OR NEW.claim_generation<OLD.claim_generation OR NEW.attempt<OLD.attempt OR
       (OLD.approved_at IS NOT NULL AND ROW(NEW.approver_id,NEW.approved_at,NEW.deadline) IS DISTINCT FROM ROW(OLD.approver_id,OLD.approved_at,OLD.deadline)) OR
       (OLD.candidate_commit_sha<>'' AND ROW(NEW.candidate_commit_sha,NEW.candidate_tree_sha,NEW.candidate_blob_sha) IS DISTINCT FROM ROW(OLD.candidate_commit_sha,OLD.candidate_tree_sha,OLD.candidate_blob_sha)) OR
       (OLD.branch_confirmed_at IS NOT NULL AND NEW.branch_confirmed_at IS DISTINCT FROM OLD.branch_confirmed_at) OR
       (OLD.pull_request_confirmed_at IS NOT NULL AND ROW(NEW.pull_request_confirmed_at,NEW.pull_request_ref,NEW.pull_request_url) IS DISTINCT FROM ROW(OLD.pull_request_confirmed_at,OLD.pull_request_ref,OLD.pull_request_url)) THEN
        RAISE EXCEPTION 'configuration writeback immutable fence violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER configuration_writeback_guard BEFORE UPDATE OR DELETE ON control_plane.managed_configuration_writebacks
    FOR EACH ROW EXECUTE FUNCTION control_plane.guard_configuration_writeback();
GRANT SELECT,INSERT,UPDATE ON control_plane.managed_configuration_writebacks TO control_plane_runtime;
RESET ROLE;
