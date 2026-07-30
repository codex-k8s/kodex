-- +goose Up
CREATE SCHEMA IF NOT EXISTS internal_rpc_authority;

CREATE TABLE internal_rpc_authority.replay_reservations (
    reservation_kind text NOT NULL
        CHECK (reservation_kind IN ('AUTHORITY_PROOF', 'AUTHORIZATION_CONTEXT')),
    jti uuid NOT NULL,
    canonical_digest_sha256 text NOT NULL
        CHECK (canonical_digest_sha256 ~ '^[a-f0-9]{64}$'),
    expires_at timestamptz NOT NULL,
    accepted_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (reservation_kind, jti)
);

CREATE INDEX replay_reservations_expiry_idx
    ON internal_rpc_authority.replay_reservations (expires_at);

CREATE TABLE internal_rpc_authority.verifier_served_snapshots (
    target_workload_id text PRIMARY KEY
        CHECK (target_workload_id ~ '^[a-z0-9](?:[a-z0-9.-]{1,94}[a-z0-9])$'),
    source_revision bigint NOT NULL CHECK (source_revision BETWEEN 1 AND 9007199254740991),
    source_digest_sha256 text NOT NULL
        CHECK (source_digest_sha256 ~ '^[a-f0-9]{64}$'),
    key_set_revision bigint NOT NULL CHECK (key_set_revision BETWEEN 1 AND 9007199254740991),
    policy_revision bigint NOT NULL CHECK (policy_revision BETWEEN 1 AND 9007199254740991),
    signer_generation bigint NOT NULL CHECK (signer_generation BETWEEN 1 AND 9007199254740991),
    served_at timestamptz NOT NULL
);

ALTER TABLE internal_rpc_authority.replay_reservations ENABLE ROW LEVEL SECURITY;
ALTER TABLE internal_rpc_authority.replay_reservations FORCE ROW LEVEL SECURITY;
ALTER TABLE internal_rpc_authority.verifier_served_snapshots ENABLE ROW LEVEL SECURITY;
ALTER TABLE internal_rpc_authority.verifier_served_snapshots FORCE ROW LEVEL SECURITY;

REVOKE ALL ON SCHEMA internal_rpc_authority FROM PUBLIC;
REVOKE ALL ON ALL TABLES IN SCHEMA internal_rpc_authority FROM PUBLIC;

-- +goose Down
-- Forward-only: откат выполняется отдельной компенсирующей миграцией.
