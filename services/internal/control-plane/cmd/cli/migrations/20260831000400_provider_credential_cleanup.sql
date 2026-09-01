-- +goose Up
SET ROLE control_plane_owner;

CREATE TABLE control_plane.provider_credential_cleanup_tasks (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE
        CHECK (ref ~ '^pcct_[A-Za-z0-9_-]{8,88}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    provider_account_id uuid NOT NULL REFERENCES control_plane.provider_accounts(id),
    provider_credential_revision_id uuid NOT NULL
        REFERENCES control_plane.provider_credential_revisions(id),
    state text NOT NULL DEFAULT 'PENDING'
        CHECK (state IN ('PENDING', 'CLAIMED', 'COMPLETED', 'DEAD_LETTER')),
    secret_name text NOT NULL
        CHECK (secret_name ~ '^[a-z0-9]([-a-z0-9]*[a-z0-9])?$' AND char_length(secret_name) <= 63),
    secret_uid uuid NOT NULL,
    secret_resource_version text NOT NULL
        CHECK (char_length(secret_resource_version) BETWEEN 1 AND 128),
    content_sha256 text NOT NULL CHECK (content_sha256 ~ '^[a-f0-9]{64}$'),
    eligible_at timestamptz NOT NULL,
    lease_owner text,
    lease_generation bigint NOT NULL DEFAULT 0 CHECK (lease_generation >= 0),
    lease_expires_at timestamptz,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    maximum_attempts integer NOT NULL DEFAULT 5 CHECK (maximum_attempts BETWEEN 1 AND 32),
    safe_error_code text NOT NULL DEFAULT '' CHECK (char_length(safe_error_code) <= 128),
    terminal_receipt text NOT NULL DEFAULT '' CHECK (char_length(terminal_receipt) <= 512),
    completed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (provider_credential_revision_id),
    CHECK (
        (state = 'PENDING'
            AND lease_owner IS NULL AND lease_expires_at IS NULL
            AND terminal_receipt = '' AND completed_at IS NULL)
        OR
        (state = 'CLAIMED'
            AND lease_owner IS NOT NULL AND char_length(lease_owner) BETWEEN 1 AND 128
            AND lease_generation > 0 AND lease_expires_at IS NOT NULL
            AND terminal_receipt = '' AND completed_at IS NULL)
        OR
        (state IN ('COMPLETED', 'DEAD_LETTER')
            AND lease_owner IS NULL AND lease_expires_at IS NULL
            AND terminal_receipt <> '' AND completed_at IS NOT NULL)
    )
);

CREATE INDEX provider_credential_cleanup_claimable
    ON control_plane.provider_credential_cleanup_tasks
        (eligible_at, lease_expires_at, created_at, id)
    WHERE state IN ('PENDING', 'CLAIMED');

CREATE INDEX provider_credential_cleanup_account_state
    ON control_plane.provider_credential_cleanup_tasks
        (provider_account_id, state, eligible_at, created_at);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.protect_provider_credential_cleanup_snapshot()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.organization_id IS DISTINCT FROM NEW.organization_id
       OR OLD.provider_account_id IS DISTINCT FROM NEW.provider_account_id
       OR OLD.provider_credential_revision_id IS DISTINCT FROM NEW.provider_credential_revision_id
       OR OLD.secret_name IS DISTINCT FROM NEW.secret_name
       OR OLD.secret_uid IS DISTINCT FROM NEW.secret_uid
       OR OLD.secret_resource_version IS DISTINCT FROM NEW.secret_resource_version
       OR OLD.content_sha256 IS DISTINCT FROM NEW.content_sha256
       OR OLD.maximum_attempts IS DISTINCT FROM NEW.maximum_attempts
       OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION 'provider credential cleanup snapshot is immutable';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER protect_provider_credential_cleanup_snapshot
BEFORE UPDATE ON control_plane.provider_credential_cleanup_tasks
FOR EACH ROW EXECUTE FUNCTION control_plane.protect_provider_credential_cleanup_snapshot();

RESET ROLE;

-- +goose Down
SET ROLE control_plane_owner;

DROP TRIGGER protect_provider_credential_cleanup_snapshot
    ON control_plane.provider_credential_cleanup_tasks;
DROP FUNCTION control_plane.protect_provider_credential_cleanup_snapshot();
DROP TABLE control_plane.provider_credential_cleanup_tasks;

RESET ROLE;
