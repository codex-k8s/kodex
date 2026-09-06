-- +goose Up
SET ROLE control_plane_owner;

CREATE TABLE control_plane.runtime_secret_drafts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^sdft_[A-Za-z0-9_-]{8,89}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    secret_id uuid NOT NULL REFERENCES control_plane.runtime_secrets(id),
    owner_actor_id uuid NOT NULL REFERENCES control_plane.subjects(id),
    staged_namespace text NOT NULL CHECK (staged_namespace ~ '^[a-z0-9]([-a-z0-9]*[a-z0-9])?$' AND length(staged_namespace)<=63),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    generation bigint GENERATED ALWAYS AS IDENTITY UNIQUE,
    state text NOT NULL CHECK (state IN ('PREPARING','DRAFT','VALID','PUBLISHING','PUBLISHED','DISCARDED','EXPIRED','FAILED')),
    expected_content_sha256 text NOT NULL CHECK (expected_content_sha256 ~ '^[a-f0-9]{64}$'),
    encrypted_descriptor jsonb CHECK (encrypted_descriptor IS NULL OR jsonb_typeof(encrypted_descriptor) = 'object' AND octet_length(encrypted_descriptor::text) < 4096),
    published_revision bigint NOT NULL DEFAULT 0 CHECK (published_revision >= 0),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at timestamptz NOT NULL DEFAULT clock_timestamp() + interval '24 hours'
);
CREATE INDEX runtime_secret_drafts_secret ON control_plane.runtime_secret_drafts(secret_id, generation DESC);
CREATE INDEX runtime_secret_drafts_expiry ON control_plane.runtime_secret_drafts(expires_at)
    WHERE state IN ('PREPARING','DRAFT','VALID','PUBLISHING');

CREATE TABLE control_plane.runtime_secret_draft_operations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^sdop_[A-Za-z0-9_-]{8,89}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    draft_id uuid NOT NULL REFERENCES control_plane.runtime_secret_drafts(id),
    actor_id uuid NOT NULL REFERENCES control_plane.subjects(id),
    kind text NOT NULL CHECK (kind IN ('SAVE','VALIDATE','PUBLISH','DISCARD')),
    state text NOT NULL DEFAULT 'PREPARED' CHECK (state IN ('PREPARED','CLAIMED','COMPLETED','FAILED')),
    expected_draft_version bigint NOT NULL CHECK (expected_draft_version > 0),
    expected_secret_version bigint NOT NULL CHECK (expected_secret_version > 0),
    expected_current_revision bigint NOT NULL CHECK (expected_current_revision >= 0),
    target_revision bigint NOT NULL DEFAULT 0 CHECK (target_revision >= 0),
    token_digest text NOT NULL UNIQUE CHECK (token_digest ~ '^[a-f0-9]{64}$'),
    idempotency_key text NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 200),
    intent_digest text NOT NULL CHECK (intent_digest ~ '^[a-f0-9]{64}$'),
    claimant_id text NOT NULL DEFAULT '',
    claim_generation bigint NOT NULL DEFAULT 0 CHECK (claim_generation >= 0),
    grant_expires_at timestamptz NOT NULL,
    lease_deadline timestamptz,
    failure_code text NOT NULL DEFAULT '',
    terminal_snapshot jsonb,
    encrypted_cleanup_descriptor jsonb,
    materialization_cleanup_descriptor jsonb,
    cleanup_completed boolean NOT NULL DEFAULT false,
    correlation_ref text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (organization_id, actor_id, kind, idempotency_key)
);
CREATE UNIQUE INDEX runtime_secret_draft_operation_active ON control_plane.runtime_secret_draft_operations(draft_id)
    WHERE state IN ('PREPARED','CLAIMED');
CREATE INDEX runtime_secret_draft_operation_recovery ON control_plane.runtime_secret_draft_operations(updated_at, ref)
    WHERE NOT cleanup_completed;
GRANT SELECT, INSERT, UPDATE ON control_plane.runtime_secret_drafts, control_plane.runtime_secret_draft_operations TO control_plane_runtime;
GRANT USAGE, SELECT ON SEQUENCE control_plane.runtime_secret_drafts_generation_seq TO control_plane_runtime;
RESET ROLE;
