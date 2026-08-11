-- +goose Up
RESET ROLE;
SET ROLE control_plane_owner;
CREATE TABLE control_plane.interaction_delivery_work (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    actor_id uuid NOT NULL,
    session_id uuid NOT NULL,
    session_version bigint NOT NULL CHECK (session_version > 0),
    turn_id uuid NOT NULL,
    turn_version bigint NOT NULL CHECK (turn_version > 0),
    attempt integer NOT NULL CHECK (attempt BETWEEN 1 AND 100),
    runtime_revision_id uuid NOT NULL,
    runtime_revision_version bigint NOT NULL CHECK (runtime_revision_version > 0),
    immutable_input_sha256 text NOT NULL CHECK (immutable_input_sha256 ~ '^[0-9a-f]{64}$'),
    kind text NOT NULL CHECK (kind IN ('FINAL_MARKDOWN','PUBLISH_ARTIFACT','PROGRESS','STATUS','INCIDENT','RUN_CARD')),
    lifecycle_state text NOT NULL,
    outcome text NOT NULL,
    artifact_id uuid,
    artifact_version bigint CHECK (artifact_version IS NULL OR artifact_version > 0),
    artifact_sha256 text NOT NULL DEFAULT '' CHECK (artifact_sha256 = '' OR artifact_sha256 ~ '^[0-9a-f]{64}$'),
    artifact_name text NOT NULL DEFAULT '',
    artifact_storage_ref text NOT NULL DEFAULT '',
    artifact_size_bytes bigint NOT NULL DEFAULT 0 CHECK (artifact_size_bytes >= 0),
    artifact_media_type text NOT NULL DEFAULT '',
    inline_payload bytea NOT NULL DEFAULT ''::bytea CHECK (octet_length(inline_payload) <= 163840),
    state text NOT NULL DEFAULT 'PENDING' CHECK (state IN ('PENDING','CLAIMED','DELIVERED','DEAD_LETTER')),
    fence bigint NOT NULL DEFAULT 0 CHECK (fence >= 0),
    lease_owner text NOT NULL DEFAULT '',
    lease_token_sha256 text NOT NULL DEFAULT '' CHECK (lease_token_sha256 = '' OR lease_token_sha256 ~ '^[0-9a-f]{64}$'),
    lease_expires_at timestamptz,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 32),
    next_attempt_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    provider_receipt_sha256 text NOT NULL DEFAULT '' CHECK (provider_receipt_sha256 = '' OR provider_receipt_sha256 ~ '^[0-9a-f]{64}$'),
    terminal_error_code text NOT NULL DEFAULT '' CHECK (length(terminal_error_code) <= 64),
    next_action text NOT NULL DEFAULT '' CHECK (length(next_action) <= 256),
    delivered_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK ((artifact_id IS NULL) = (artifact_version IS NULL)),
    CHECK ((artifact_id IS NULL) = (artifact_sha256 = '')),
    CHECK (octet_length(inline_payload) = 0 OR artifact_storage_ref LIKE 'control-plane-inline:%')
);
ALTER TABLE control_plane.interaction_delivery_work OWNER TO control_plane_owner;
GRANT SELECT, INSERT, UPDATE ON control_plane.interaction_delivery_work TO control_plane_runtime;
CREATE INDEX interaction_delivery_work_due_idx ON control_plane.interaction_delivery_work
    (next_attempt_at, created_at) WHERE state IN ('PENDING','CLAIMED');

-- +goose Down
-- Forward-only: owner-produced delivery receipts are retained.
SELECT 1;
