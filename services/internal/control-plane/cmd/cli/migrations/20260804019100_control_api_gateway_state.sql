-- +goose Up
-- Durable verifying-side session fence и public TLS served watermark принадлежат
-- control-plane; gateway не получает прямой PostgreSQL path.
RESET ROLE;
SET ROLE control_plane_owner;
SET search_path = pg_catalog, control_plane;

CREATE TABLE control_plane.owner_oidc_sessions (
    organization_id uuid NOT NULL,
    actor_id uuid NOT NULL,
    session_id uuid NOT NULL,
    credential_digest_sha256 text NOT NULL CHECK (credential_digest_sha256 ~ '^[a-f0-9]{64}$'),
    current_revision bigint NOT NULL CHECK (current_revision BETWEEN 1 AND 9007199254740991),
    revoked_at timestamptz,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (organization_id, actor_id, session_id)
);

CREATE TABLE control_plane.gateway_public_tls_state (
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    workload_id text NOT NULL CHECK (workload_id ~ '^[a-z0-9][a-z0-9._-]{0,127}$'),
    generation bigint NOT NULL CHECK (generation BETWEEN 1 AND 9007199254740991),
    certificate_sha256 text NOT NULL CHECK (certificate_sha256 ~ '^[a-f0-9]{64}$'),
    not_before timestamptz NOT NULL,
    not_after timestamptz NOT NULL CHECK (not_after > not_before),
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (organization_id, project_id, workload_id)
);

ALTER TABLE control_plane.owner_oidc_sessions ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.owner_oidc_sessions FORCE ROW LEVEL SECURITY;
ALTER TABLE control_plane.gateway_public_tls_state ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.gateway_public_tls_state FORCE ROW LEVEL SECURITY;

CREATE POLICY owner_oidc_sessions_runtime_scope
    ON control_plane.owner_oidc_sessions
    FOR ALL TO control_plane_runtime
    USING (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND actor_id = (control_plane.runtime_scope()).actor_id
    )
    WITH CHECK (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND actor_id = (control_plane.runtime_scope()).actor_id
    );

CREATE POLICY gateway_public_tls_runtime_scope
    ON control_plane.gateway_public_tls_state
    FOR ALL TO control_plane_runtime
    USING (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id = (control_plane.runtime_scope()).project_id
    )
    WITH CHECK (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id = (control_plane.runtime_scope()).project_id
    );

REVOKE ALL ON control_plane.owner_oidc_sessions,
    control_plane.gateway_public_tls_state FROM PUBLIC;
GRANT SELECT, INSERT, UPDATE ON control_plane.owner_oidc_sessions,
    control_plane.gateway_public_tls_state TO control_plane_runtime;

UPDATE control_plane.schema_state
SET version = 20260804019100, migrated_at = clock_timestamp()
WHERE singleton = true;
RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
    RAISE EXCEPTION 'migration 20260804019100 is forward-only: owner session revocation and served TLS watermarks cannot be discarded';
END
$$;
-- +goose StatementEnd
