-- +goose Up
SET ROLE control_plane_owner;

ALTER TABLE control_plane.provider_accounts ADD CONSTRAINT provider_accounts_renewal_owner_key UNIQUE (organization_id, id);
ALTER TABLE control_plane.provider_credential_revisions ADD CONSTRAINT provider_credentials_renewal_owner_key UNIQUE (organization_id, provider_account_id, id);

CREATE TABLE control_plane.provider_credential_renewal_tasks (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^pcrt_[A-Za-z0-9_-]{8,84}$'),
    organization_id uuid NOT NULL,
    provider_account_id uuid NOT NULL,
    account_version bigint NOT NULL CHECK (account_version BETWEEN 1 AND 9007199254740991),
    input_credential_id uuid NOT NULL,
    attempt bigint NOT NULL CHECK (attempt BETWEEN 1 AND 9007199254740991),
    state text NOT NULL CHECK (state IN ('CLAIMED', 'AUTHORIZED', 'SUCCEEDED', 'UNKNOWN_OUTCOME', 'FAILED', 'CANCELLED')),
    claimant_id text NOT NULL CHECK (length(claimant_id) BETWEEN 1 AND 128),
    generation bigint NOT NULL CHECK (generation BETWEEN 1 AND 9007199254740991),
    fence text NOT NULL CHECK (fence ~ '^prf_[A-Za-z0-9_-]{8,84}$'),
    request_digest text NOT NULL CHECK (request_digest ~ '^[a-f0-9]{64}$'),
    owner_request_digest text NOT NULL CHECK (owner_request_digest ~ '^[a-f0-9]{64}$'),
    expires_at timestamptz NOT NULL,
    authorized_at timestamptz,
    completed_at timestamptz,
    output_credential_id uuid,
    output_account_version bigint,
    materializer_receipt_uid uuid,
    materializer_receipt_version text,
    result_digest text,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    FOREIGN KEY (organization_id, provider_account_id) REFERENCES control_plane.provider_accounts(organization_id, id),
    FOREIGN KEY (organization_id, provider_account_id, input_credential_id) REFERENCES control_plane.provider_credential_revisions(organization_id, provider_account_id, id),
    FOREIGN KEY (organization_id, provider_account_id, output_credential_id) REFERENCES control_plane.provider_credential_revisions(organization_id, provider_account_id, id),
    UNIQUE (provider_account_id, attempt),
    CHECK (expires_at > created_at),
    CHECK ((state IN ('CLAIMED', 'AUTHORIZED')) = (completed_at IS NULL)),
    CHECK (state NOT IN ('AUTHORIZED', 'SUCCEEDED', 'UNKNOWN_OUTCOME') OR authorized_at IS NOT NULL),
    CHECK (state <> 'FAILED' OR authorized_at IS NULL),
    CHECK ((state = 'SUCCEEDED') = (output_credential_id IS NOT NULL)),
    CHECK ((output_credential_id IS NULL AND output_account_version IS NULL AND materializer_receipt_uid IS NULL AND materializer_receipt_version IS NULL AND result_digest IS NULL)
        OR (output_credential_id IS NOT NULL AND output_account_version IS NOT NULL AND output_account_version BETWEEN 1 AND 9007199254740991
            AND materializer_receipt_uid IS NOT NULL AND materializer_receipt_version IS NOT NULL
            AND length(materializer_receipt_version) BETWEEN 1 AND 128 AND result_digest IS NOT NULL AND result_digest ~ '^[a-f0-9]{64}$'))
);
CREATE UNIQUE INDEX provider_renewal_one_active ON control_plane.provider_credential_renewal_tasks(provider_account_id) WHERE state IN ('CLAIMED', 'AUTHORIZED');
CREATE UNIQUE INDEX provider_renewal_one_possible_effect ON control_plane.provider_credential_renewal_tasks(input_credential_id) WHERE authorized_at IS NOT NULL;
CREATE INDEX provider_renewal_writer_proof ON control_plane.provider_credential_renewal_tasks(request_digest);
CREATE INDEX provider_renewal_owner_proof ON control_plane.provider_credential_renewal_tasks(owner_request_digest);
CREATE INDEX provider_renewal_recovery ON control_plane.provider_credential_renewal_tasks(state, created_at, id) WHERE state = 'UNKNOWN_OUTCOME';

-- +goose StatementBegin
CREATE FUNCTION control_plane.protect_provider_renewal_task() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF (OLD.ref, OLD.organization_id, OLD.provider_account_id, OLD.account_version, OLD.input_credential_id,
        OLD.attempt, OLD.claimant_id, OLD.generation, OLD.fence, OLD.request_digest, OLD.owner_request_digest, OLD.expires_at, OLD.created_at)
       IS DISTINCT FROM
       (NEW.ref, NEW.organization_id, NEW.provider_account_id, NEW.account_version, NEW.input_credential_id,
        NEW.attempt, NEW.claimant_id, NEW.generation, NEW.fence, NEW.request_digest, NEW.owner_request_digest, NEW.expires_at, NEW.created_at)
       OR (OLD.authorized_at IS NOT NULL AND OLD.authorized_at IS DISTINCT FROM NEW.authorized_at)
       OR (OLD.state = 'CLAIMED' AND NEW.state NOT IN ('AUTHORIZED', 'FAILED', 'CANCELLED'))
       OR (OLD.state = 'AUTHORIZED' AND NEW.state NOT IN ('SUCCEEDED', 'UNKNOWN_OUTCOME', 'CANCELLED'))
       OR (OLD.state = 'UNKNOWN_OUTCOME' AND NEW.state NOT IN ('SUCCEEDED', 'CANCELLED'))
       OR OLD.state IN ('SUCCEEDED', 'FAILED', 'CANCELLED') THEN
        RAISE EXCEPTION 'provider renewal task binding is immutable';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER protect_provider_renewal_task BEFORE UPDATE ON control_plane.provider_credential_renewal_tasks FOR EACH ROW EXECUTE FUNCTION control_plane.protect_provider_renewal_task();

-- +goose StatementBegin
CREATE FUNCTION control_plane.cancel_stale_provider_renewal() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.version IS DISTINCT FROM NEW.version OR OLD.state IS DISTINCT FROM NEW.state OR OLD.enabled IS DISTINCT FROM NEW.enabled
        OR OLD.current_credential_revision_id IS DISTINCT FROM NEW.current_credential_revision_id THEN
        UPDATE control_plane.provider_credential_renewal_tasks SET state = 'CANCELLED', completed_at = clock_timestamp()
        WHERE provider_account_id = NEW.id AND state IN ('CLAIMED', 'AUTHORIZED', 'UNKNOWN_OUTCOME');
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER cancel_stale_provider_renewal AFTER UPDATE ON control_plane.provider_accounts FOR EACH ROW EXECUTE FUNCTION control_plane.cancel_stale_provider_renewal();

RESET ROLE;

-- +goose Down
-- Forward-only: удаление журнала допускает повтор внешнего OAuth effect.
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'provider credential renewal requires forward-only recovery'; END $$;
-- +goose StatementEnd
