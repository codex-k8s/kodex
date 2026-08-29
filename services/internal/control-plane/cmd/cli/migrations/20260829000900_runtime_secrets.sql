-- +goose Up
SET ROLE control_plane_owner;

ALTER TABLE control_plane.access_bindings
    DROP CONSTRAINT access_bindings_resource_kind_check;
ALTER TABLE control_plane.access_bindings
    ADD CONSTRAINT access_bindings_resource_kind_check
    CHECK (resource_kind IS NULL OR resource_kind IN ('PROJECT', 'AGENT', 'WORKFLOW', 'RUN', 'OWNER_GATE', 'ARTIFACT', 'SCHEDULE', 'INTEGRATION', 'SECRET'));

INSERT INTO control_plane.permission_registry
    (permission_key, name_key, description_key, risk, allowed_scopes, resource_kinds, owner_condition_supported)
VALUES
    ('secret.view', 'i18n:PERMISSION_SECRET_VIEW_NAME', 'i18n:PERMISSION_SECRET_VIEW_DESCRIPTION', 'READ', ARRAY['ORGANIZATION','PROJECT','RESOURCE_KIND','RESOURCE_INSTANCE'], ARRAY['PROJECT','SECRET'], false),
    ('secret.create', 'i18n:PERMISSION_SECRET_CREATE_NAME', 'i18n:PERMISSION_SECRET_CREATE_DESCRIPTION', 'WRITE', ARRAY['ORGANIZATION','PROJECT'], ARRAY['PROJECT'], false),
    ('secret.rotate', 'i18n:PERMISSION_SECRET_ROTATE_NAME', 'i18n:PERMISSION_SECRET_ROTATE_DESCRIPTION', 'ADMIN', ARRAY['ORGANIZATION','PROJECT','RESOURCE_KIND','RESOURCE_INSTANCE'], ARRAY['SECRET'], false),
    ('secret.revoke', 'i18n:PERMISSION_SECRET_REVOKE_NAME', 'i18n:PERMISSION_SECRET_REVOKE_DESCRIPTION', 'ADMIN', ARRAY['ORGANIZATION','PROJECT','RESOURCE_KIND','RESOURCE_INSTANCE'], ARRAY['SECRET'], false),
    ('secret.reveal', 'i18n:PERMISSION_SECRET_REVEAL_NAME', 'i18n:PERMISSION_SECRET_REVEAL_DESCRIPTION', 'ADMIN', ARRAY['ORGANIZATION','PROJECT','RESOURCE_KIND','RESOURCE_INSTANCE'], ARRAY['SECRET'], false)
ON CONFLICT (permission_key) DO NOTHING;

-- Existing immutable system role versions are superseded atomically. User
-- bindings remain pinned to an explicit version and move in the same change.
-- +goose StatementBegin
DO $$
DECLARE
    role_row record;
    next_version_id uuid;
    next_permissions text[];
    next_ref text;
BEGIN
    FOR role_row IN
        SELECT application_role.id AS role_id, application_role.organization_id,
               application_role.current_version_id, application_role.stable_key,
               role_version.revision, role_version.name, role_version.description,
               role_version.permission_keys, role_version.allowed_scopes,
               role_version.created_by
        FROM control_plane.application_roles application_role
        JOIN control_plane.application_role_versions role_version
          ON role_version.id = application_role.current_version_id
        WHERE application_role.stable_key IN ('OWNER', 'ADMINISTRATOR', 'AUDITOR')
    LOOP
        IF role_row.stable_key = 'AUDITOR' THEN
            next_permissions := ARRAY(SELECT DISTINCT value FROM unnest(role_row.permission_keys || ARRAY['secret.view']) value ORDER BY value);
        ELSE
            next_permissions := ARRAY(SELECT DISTINCT value FROM unnest(role_row.permission_keys || ARRAY['secret.view','secret.create','secret.rotate','secret.revoke','secret.reveal']) value ORDER BY value);
        END IF;
        next_ref := 'arv_secret_' || substr(md5(role_row.role_id::text), 1, 20);
        INSERT INTO control_plane.application_role_versions
            (ref, organization_id, role_id, revision, name, description, permission_keys,
             allowed_scopes, change_comment, created_by)
        VALUES
            (next_ref, role_row.organization_id, role_row.role_id, role_row.revision + 1,
             role_row.name, role_row.description, next_permissions,
             role_row.allowed_scopes, 'i18n:SYSTEM_ROLE_RUNTIME_SECRETS', role_row.created_by)
        RETURNING id INTO next_version_id;

        UPDATE control_plane.application_roles
        SET current_version_id = next_version_id, version = version + 1, updated_at = clock_timestamp()
        WHERE id = role_row.role_id;

        UPDATE control_plane.access_bindings
        SET role_version_id = next_version_id, version = version + 1, updated_at = clock_timestamp()
        WHERE role_version_id = role_row.current_version_id AND state = 'ACTIVE';
    END LOOP;
END $$;
-- +goose StatementEnd

CREATE TABLE control_plane.runtime_secrets (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^sec_[A-Za-z0-9_-]{8,92}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    project_id uuid NOT NULL REFERENCES control_plane.projects(id),
    namespace text NOT NULL CHECK (namespace ~ '^[a-z0-9]([-a-z0-9]*[a-z0-9])?$'),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 120),
    description text NOT NULL DEFAULT '' CHECK (char_length(description) <= 1000),
    value_type text NOT NULL CHECK (value_type IN ('STRING', 'BINARY', 'JSON')),
    state text NOT NULL CHECK (state IN ('PROVISIONING', 'ACTIVE', 'REVOKED')),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    current_revision bigint NOT NULL DEFAULT 0 CHECK (current_revision >= 0),
    display_hint_prefix text NOT NULL DEFAULT '' CHECK (char_length(display_hint_prefix) <= 6),
    display_hint_suffix text NOT NULL DEFAULT '' CHECK (char_length(display_hint_suffix) <= 6),
    created_by uuid NOT NULL REFERENCES control_plane.subjects(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (project_id, name)
);

CREATE INDEX runtime_secrets_project_catalog
    ON control_plane.runtime_secrets (project_id, updated_at DESC, id DESC);

CREATE TABLE control_plane.runtime_secret_revisions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^secr_[A-Za-z0-9_-]{8,91}$'),
    secret_id uuid NOT NULL REFERENCES control_plane.runtime_secrets(id),
    revision bigint NOT NULL CHECK (revision > 0),
    namespace text NOT NULL CHECK (namespace ~ '^[a-z0-9]([-a-z0-9]*[a-z0-9])?$'),
    secret_name text NOT NULL CHECK (secret_name ~ '^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$'),
    secret_key text NOT NULL CHECK (char_length(secret_key) BETWEEN 1 AND 253),
    secret_uid text NOT NULL CHECK (char_length(secret_uid) BETWEEN 1 AND 128),
    secret_resource_version text NOT NULL CHECK (char_length(secret_resource_version) BETWEEN 1 AND 128),
    content_sha256 text NOT NULL CHECK (content_sha256 ~ '^[a-f0-9]{64}$'),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (secret_id, revision),
    UNIQUE (namespace, secret_name)
);

CREATE TABLE control_plane.runtime_secret_operations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^secop_[A-Za-z0-9_-]{8,90}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    project_id uuid NOT NULL REFERENCES control_plane.projects(id),
    actor_id uuid NOT NULL REFERENCES control_plane.subjects(id),
    secret_id uuid NOT NULL REFERENCES control_plane.runtime_secrets(id),
    kind text NOT NULL CHECK (kind IN ('CREATE', 'ROTATE', 'REVEAL', 'REVOKE')),
    target_revision bigint NOT NULL CHECK (target_revision > 0),
    expected_secret_version bigint NOT NULL CHECK (expected_secret_version > 0),
    expected_current_revision bigint NOT NULL CHECK (expected_current_revision >= 0),
    expected_content_sha256 text CHECK (
        (kind IN ('CREATE', 'ROTATE') AND expected_content_sha256 ~ '^[a-f0-9]{64}$') OR
        (kind IN ('REVEAL', 'REVOKE') AND expected_content_sha256 IS NULL)
    ),
    token_digest text NOT NULL UNIQUE CHECK (token_digest ~ '^[a-f0-9]{64}$'),
    idempotency_key text NOT NULL CHECK (char_length(idempotency_key) BETWEEN 8 AND 128),
    intent_digest text NOT NULL CHECK (intent_digest ~ '^[a-f0-9]{64}$'),
    correlation_ref text NOT NULL CHECK (char_length(correlation_ref) BETWEEN 1 AND 160),
    state text NOT NULL CHECK (state IN ('PREPARED', 'CLAIMED', 'COMPLETED', 'FAILED')),
    grant_expires_at timestamptz NOT NULL,
    claimant_id text CHECK (claimant_id IS NULL OR char_length(claimant_id) BETWEEN 1 AND 128),
    claim_generation bigint NOT NULL DEFAULT 0 CHECK (claim_generation >= 0),
    claim_lease_deadline timestamptz,
    claimed_at timestamptz,
    terminal_error_code text CHECK (terminal_error_code IS NULL OR terminal_error_code IN (
        'KUBERNETES_UNAVAILABLE', 'MATERIALIZATION_CONFLICT', 'MATERIALIZATION_INVALID',
        'STALE_SECRET_VERSION', 'RECONCILIATION_FAILED', 'GRANT_EXPIRED'
    )),
    terminal_secret_snapshot jsonb CHECK (terminal_secret_snapshot IS NULL OR jsonb_typeof(terminal_secret_snapshot) = 'object'),
    terminal_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (organization_id, actor_id, kind, idempotency_key),
    CHECK (
        (state = 'PREPARED' AND claimant_id IS NULL AND claim_lease_deadline IS NULL AND terminal_at IS NULL AND terminal_error_code IS NULL AND terminal_secret_snapshot IS NULL) OR
        (state = 'CLAIMED' AND claimant_id IS NOT NULL AND claim_generation > 0 AND claim_lease_deadline IS NOT NULL AND terminal_at IS NULL AND terminal_error_code IS NULL AND terminal_secret_snapshot IS NULL) OR
        (state = 'COMPLETED' AND claimant_id IS NOT NULL AND claim_generation > 0 AND terminal_at IS NOT NULL AND terminal_error_code IS NULL AND terminal_secret_snapshot IS NOT NULL) OR
        (state = 'FAILED' AND terminal_at IS NOT NULL AND terminal_error_code IS NOT NULL AND terminal_secret_snapshot IS NULL AND (
            (terminal_error_code = 'GRANT_EXPIRED' AND claimant_id IS NULL AND claim_generation = 0 AND claim_lease_deadline IS NULL) OR
            (terminal_error_code <> 'GRANT_EXPIRED' AND claimant_id IS NOT NULL AND claim_generation > 0)
        ))
    )
);

CREATE UNIQUE INDEX runtime_secret_single_mutation
    ON control_plane.runtime_secret_operations (secret_id)
    WHERE kind IN ('CREATE', 'ROTATE', 'REVOKE') AND state IN ('PREPARED', 'CLAIMED');
CREATE INDEX runtime_secret_operations_grant_expiry
    ON control_plane.runtime_secret_operations (state, grant_expires_at);
CREATE INDEX runtime_secret_operations_claim_expiry
    ON control_plane.runtime_secret_operations (state, claim_lease_deadline)
    WHERE state = 'CLAIMED';

CREATE TABLE control_plane.runtime_secret_operation_audits (
    operation_id uuid PRIMARY KEY REFERENCES control_plane.runtime_secret_operations(id) ON DELETE CASCADE,
    audit_event_id uuid NOT NULL UNIQUE REFERENCES control_plane.audit_events(id) ON DELETE CASCADE
);

RESET ROLE;

-- +goose Down
SET ROLE control_plane_owner;

DELETE FROM control_plane.access_bindings WHERE resource_kind = 'SECRET';
DELETE FROM control_plane.audit_events WHERE action LIKE 'runtime-secret.%';

DROP TABLE control_plane.runtime_secret_operation_audits;
DROP TABLE control_plane.runtime_secret_operations;
DROP TABLE control_plane.runtime_secret_revisions;
DROP TABLE control_plane.runtime_secrets;

-- Restore system roles to the exact immutable version superseded by Up.
-- +goose StatementBegin
DO $$
DECLARE
    migrated_role record;
    previous_version_id uuid;
BEGIN
    FOR migrated_role IN
        SELECT role.id AS role_id, role.current_version_id, version.revision
        FROM control_plane.application_roles role
        JOIN control_plane.application_role_versions version ON version.id = role.current_version_id
        WHERE version.change_comment = 'i18n:SYSTEM_ROLE_RUNTIME_SECRETS'
    LOOP
        SELECT id INTO STRICT previous_version_id
        FROM control_plane.application_role_versions
        WHERE role_id = migrated_role.role_id AND revision = migrated_role.revision - 1;

        UPDATE control_plane.access_bindings
        SET role_version_id = previous_version_id, version = version + 1, updated_at = clock_timestamp()
        WHERE role_version_id = migrated_role.current_version_id;

        UPDATE control_plane.application_roles
        SET current_version_id = previous_version_id, version = version + 1, updated_at = clock_timestamp()
        WHERE id = migrated_role.role_id;
    END LOOP;
END $$;
-- +goose StatementEnd

ALTER TABLE control_plane.application_role_versions DISABLE TRIGGER protect_application_role_version;
DELETE FROM control_plane.application_role_versions
WHERE change_comment = 'i18n:SYSTEM_ROLE_RUNTIME_SECRETS';
ALTER TABLE control_plane.application_role_versions ENABLE TRIGGER protect_application_role_version;

DELETE FROM control_plane.permission_registry
WHERE permission_key IN ('secret.view', 'secret.create', 'secret.rotate', 'secret.revoke', 'secret.reveal');

ALTER TABLE control_plane.access_bindings
    DROP CONSTRAINT access_bindings_resource_kind_check;
ALTER TABLE control_plane.access_bindings
    ADD CONSTRAINT access_bindings_resource_kind_check
    CHECK (resource_kind IS NULL OR resource_kind IN ('PROJECT', 'AGENT', 'WORKFLOW', 'RUN', 'OWNER_GATE', 'ARTIFACT', 'SCHEDULE', 'INTEGRATION'));

RESET ROLE;
