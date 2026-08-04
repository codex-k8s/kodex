-- +goose Up
-- +goose StatementBegin
DO $roles$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'interaction_gateway_runtime') THEN
        CREATE ROLE interaction_gateway_runtime NOLOGIN;
    END IF;
END
$roles$;
-- +goose StatementEnd

ALTER ROLE interaction_gateway_runtime
    NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;

CREATE TABLE interaction_gateway_metadata (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    schema_version bigint NOT NULL CHECK (schema_version = 1)
);
INSERT INTO interaction_gateway_metadata (singleton, schema_version) VALUES (true, 1);

CREATE TABLE interaction_gateway_inbound_events (
    id uuid PRIMARY KEY,
    provider_event_id text NOT NULL UNIQUE CHECK (length(provider_event_id) BETWEEN 1 AND 256),
    kind text NOT NULL CHECK (kind IN ('POST', 'SLASH', 'ACTION', 'DIALOG', 'REACTION')),
    revision bigint NOT NULL CHECK (revision > 0),
    payload jsonb NOT NULL,
    digest_sha256 text NOT NULL CHECK (digest_sha256 ~ '^[0-9a-f]{64}$'),
    state text NOT NULL CHECK (state IN ('PENDING', 'PROCESSING', 'WAITING_SCAN', 'COMPLETED', 'IGNORED', 'FAILED')),
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    session_id uuid,
    prompt_artifact_id uuid,
    attachment_artifacts jsonb NOT NULL DEFAULT '[]'::jsonb,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 32),
    processing_expires_at timestamptz,
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    last_error_code text NOT NULL DEFAULT '' CHECK (length(last_error_code) <= 64),
    turn_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (state <> 'COMPLETED' OR turn_id IS NOT NULL),
    CHECK (state <> 'WAITING_SCAN' OR prompt_artifact_id IS NOT NULL)
);
CREATE INDEX interaction_gateway_inbound_due_idx
    ON interaction_gateway_inbound_events (next_attempt_at, created_at)
    WHERE state IN ('PENDING', 'PROCESSING', 'WAITING_SCAN');

CREATE TABLE interaction_gateway_cursors (
    channel_id text PRIMARY KEY CHECK (length(channel_id) BETWEEN 1 AND 64),
    last_post_at bigint NOT NULL CHECK (last_post_at >= 0),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE interaction_gateway_deliveries (
    id uuid PRIMARY KEY,
    kind text NOT NULL CHECK (kind IN ('RUN', 'STATUS', 'INCIDENT', 'OWNER_DECISION', 'ARTIFACT')),
    state text NOT NULL CHECK (state IN ('PENDING', 'DELIVERING', 'PROVIDER_ACCEPTED', 'DELIVERED', 'DEAD_LETTER')),
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    session_id uuid,
    turn_id uuid,
    attempt integer CHECK (attempt IS NULL OR attempt BETWEEN 1 AND 100),
    immutable_input_sha256 text NOT NULL DEFAULT '' CHECK (immutable_input_sha256 = '' OR immutable_input_sha256 ~ '^[0-9a-f]{64}$'),
    team_id text NOT NULL CHECK (length(team_id) BETWEEN 1 AND 64),
    channel_id text NOT NULL CHECK (length(channel_id) BETWEEN 1 AND 64),
    root_post_id text NOT NULL DEFAULT '' CHECK (length(root_post_id) <= 64),
    bot_stable_key text NOT NULL CHECK (bot_stable_key ~ '^[a-z0-9][a-z0-9-]{0,63}$'),
    locale text NOT NULL CHECK (locale IN ('en', 'ru')),
    payload jsonb NOT NULL,
    payload_sha256 text NOT NULL CHECK (payload_sha256 ~ '^[0-9a-f]{64}$'),
    attachments jsonb NOT NULL DEFAULT '[]'::jsonb,
    provider_post_id text NOT NULL DEFAULT '' CHECK (length(provider_post_id) <= 64),
    provider_receipt_sha256 text NOT NULL DEFAULT '' CHECK (provider_receipt_sha256 = '' OR provider_receipt_sha256 ~ '^[0-9a-f]{64}$'),
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 32),
    fence bigint NOT NULL DEFAULT 0 CHECK (fence >= 0),
    lease_owner text NOT NULL DEFAULT '' CHECK (length(lease_owner) <= 128),
    lease_token_sha256 text NOT NULL DEFAULT '' CHECK (lease_token_sha256 = '' OR lease_token_sha256 ~ '^[0-9a-f]{64}$'),
    lease_expires_at timestamptz,
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    last_error_code text NOT NULL DEFAULT '' CHECK (length(last_error_code) <= 64),
    owner_gate_id uuid,
    owner_gate_version bigint CHECK (owner_gate_version IS NULL OR owner_gate_version > 0),
    process_run_id uuid,
    process_version bigint CHECK (process_version IS NULL OR process_version > 0),
    owner_gate_claim_token_ciphertext bytea,
    owner_gate_claim_fence bigint CHECK (owner_gate_claim_fence IS NULL OR owner_gate_claim_fence > 0),
    owner_gate_claim_expires_at timestamptz,
    recipient_actor_id uuid,
    owner_gate_payload_sha256 text NOT NULL DEFAULT '' CHECK (owner_gate_payload_sha256 = '' OR owner_gate_payload_sha256 ~ '^[0-9a-f]{64}$'),
    delivery_recorded_at timestamptz,
    owner_gate_decided_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (owner_gate_id IS NOT NULL OR owner_gate_claim_token_ciphertext IS NULL),
    CHECK (owner_gate_id IS NULL OR state = 'DELIVERED' OR owner_gate_claim_token_ciphertext IS NOT NULL),
    CHECK ((owner_gate_id IS NULL) = (owner_gate_payload_sha256 = '')),
    CHECK (state <> 'DELIVERED' OR (provider_post_id <> '' AND provider_receipt_sha256 <> ''))
);
CREATE INDEX interaction_gateway_deliveries_due_idx
    ON interaction_gateway_deliveries (next_attempt_at, created_at)
    WHERE state IN ('PENDING', 'DELIVERING', 'PROVIDER_ACCEPTED');

CREATE TABLE interaction_gateway_owner_gate_claim_requests (
    idempotency_key uuid PRIMARY KEY,
    state text NOT NULL CHECK (state IN ('PENDING', 'CLAIMED', 'COMPLETED')),
    owner_gate_id uuid,
    delivery_id uuid REFERENCES interaction_gateway_deliveries(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (state <> 'CLAIMED' OR (owner_gate_id IS NOT NULL AND delivery_id IS NOT NULL))
);
CREATE UNIQUE INDEX interaction_gateway_single_pending_gate_claim_idx
    ON interaction_gateway_owner_gate_claim_requests ((true)) WHERE state = 'PENDING';

GRANT USAGE ON SCHEMA public TO interaction_gateway_runtime;
GRANT SELECT, INSERT, UPDATE, DELETE ON
    interaction_gateway_metadata,
    interaction_gateway_inbound_events,
    interaction_gateway_cursors,
    interaction_gateway_deliveries,
    interaction_gateway_owner_gate_claim_requests
TO interaction_gateway_runtime;

-- +goose Down
-- Forward-only: dedup, cursor, provider receipts и delivery fences не удаляются.
SELECT 1;
