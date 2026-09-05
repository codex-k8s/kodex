-- +goose Up
SET ROLE control_plane_owner;

CREATE TABLE control_plane.provider_model_catalog_tasks (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^mcattsk_[A-Za-z0-9_-]{8,84}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    provider_account_id uuid NOT NULL REFERENCES control_plane.provider_accounts(id),
    account_version bigint NOT NULL CHECK (account_version BETWEEN 1 AND 9007199254740991),
    provider_credential_revision_id uuid NOT NULL REFERENCES control_plane.provider_credential_revisions(id),
    authorization_method text NOT NULL CHECK (authorization_method IN ('API_KEY', 'DEVICE_CODE')),
    state text NOT NULL DEFAULT 'PENDING' CHECK (state IN ('PENDING', 'CLAIMED', 'COMPLETED', 'CANCELLED')),
    claimant_id text NOT NULL DEFAULT '',
    claim_generation bigint NOT NULL DEFAULT 0 CHECK (claim_generation BETWEEN 0 AND 9007199254740991),
    fence text NOT NULL DEFAULT '',
    request_digest text NOT NULL DEFAULT '',
    expires_at timestamptz,
    completed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK ((state = 'CLAIMED' AND length(claimant_id) BETWEEN 1 AND 128 AND claim_generation > 0
            AND length(fence) BETWEEN 8 AND 128 AND request_digest ~ '^[a-f0-9]{64}$' AND expires_at IS NOT NULL)
        OR (state <> 'CLAIMED' AND claimant_id = '' AND fence = '' AND request_digest = '' AND expires_at IS NULL)),
    CHECK ((state IN ('COMPLETED', 'CANCELLED')) = (completed_at IS NOT NULL))
);
CREATE UNIQUE INDEX provider_model_catalog_one_active ON control_plane.provider_model_catalog_tasks(provider_account_id)
    WHERE state IN ('PENDING', 'CLAIMED');
CREATE INDEX provider_model_catalog_proof_lookup ON control_plane.provider_model_catalog_tasks(request_digest)
    WHERE state = 'CLAIMED';

CREATE TABLE control_plane.provider_model_catalog_observations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id uuid NOT NULL UNIQUE REFERENCES control_plane.provider_model_catalog_tasks(id),
    request_digest text NOT NULL CHECK (request_digest ~ '^[a-f0-9]{64}$'),
    receipt_digest text NOT NULL CHECK (receipt_digest ~ '^[a-f0-9]{64}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    provider_account_id uuid NOT NULL REFERENCES control_plane.provider_accounts(id),
    account_version bigint NOT NULL CHECK (account_version BETWEEN 1 AND 9007199254740991),
    provider_credential_revision_id uuid NOT NULL REFERENCES control_plane.provider_credential_revisions(id),
    source text NOT NULL CHECK (source IN ('REMOTE_API', 'REMOTE_CODEX', '')),
    failure text NOT NULL CHECK (failure IN ('NONE', 'UNAVAILABLE', 'UNVERIFIED_SOURCE', 'AUTHORIZATION_REJECTED')),
    models jsonb NOT NULL CHECK (jsonb_typeof(models) = 'array' AND jsonb_array_length(models) <= 128 AND octet_length(models::text) <= 131072),
    content_digest text NOT NULL CHECK (content_digest ~ '^[a-f0-9]{64}$'),
    observed_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL CHECK (expires_at > observed_at),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK ((failure = 'NONE' AND source <> '') OR (failure <> 'NONE' AND source = '' AND models = '[]'::jsonb))
);
CREATE INDEX provider_model_catalog_latest ON control_plane.provider_model_catalog_observations(provider_account_id, created_at DESC, id DESC);

-- +goose StatementBegin
CREATE FUNCTION control_plane.protect_provider_model_catalog_observation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'provider model catalog observation is immutable';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER protect_provider_model_catalog_observation BEFORE UPDATE OR DELETE ON control_plane.provider_model_catalog_observations
    FOR EACH ROW EXECUTE FUNCTION control_plane.protect_provider_model_catalog_observation();

-- +goose StatementBegin
CREATE FUNCTION control_plane.protect_provider_model_catalog_task() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.ref IS DISTINCT FROM NEW.ref OR OLD.organization_id IS DISTINCT FROM NEW.organization_id
       OR OLD.provider_account_id IS DISTINCT FROM NEW.provider_account_id OR OLD.account_version IS DISTINCT FROM NEW.account_version
       OR OLD.provider_credential_revision_id IS DISTINCT FROM NEW.provider_credential_revision_id
       OR OLD.authorization_method IS DISTINCT FROM NEW.authorization_method OR OLD.created_at IS DISTINCT FROM NEW.created_at
       OR NEW.claim_generation < OLD.claim_generation
       OR OLD.state IN ('COMPLETED', 'CANCELLED') THEN
        RAISE EXCEPTION 'provider model catalog task binding is immutable';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER protect_provider_model_catalog_task BEFORE UPDATE ON control_plane.provider_model_catalog_tasks
    FOR EACH ROW EXECUTE FUNCTION control_plane.protect_provider_model_catalog_task();

RESET ROLE;

-- +goose Down
SET ROLE control_plane_owner;
DROP TABLE control_plane.provider_model_catalog_observations;
DROP TABLE control_plane.provider_model_catalog_tasks;
DROP FUNCTION control_plane.protect_provider_model_catalog_observation();
DROP FUNCTION control_plane.protect_provider_model_catalog_task();
RESET ROLE;
