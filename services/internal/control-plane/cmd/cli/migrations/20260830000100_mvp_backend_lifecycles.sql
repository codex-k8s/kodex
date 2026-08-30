-- +goose Up
SET ROLE control_plane_owner;

ALTER TABLE control_plane.access_bindings
    DROP CONSTRAINT access_bindings_resource_kind_check;
ALTER TABLE control_plane.access_bindings
    ADD CONSTRAINT access_bindings_resource_kind_check
    CHECK (resource_kind IS NULL OR resource_kind IN (
        'PROJECT', 'AGENT', 'WORKFLOW', 'RUN', 'SESSION', 'OWNER_GATE',
        'ARTIFACT', 'SCHEDULE', 'INTEGRATION', 'RUNTIME_ENVIRONMENT',
        'ROLE_IMAGE', 'SECRET', 'PROVIDER_ACCOUNT'
    ));

INSERT INTO control_plane.permission_registry
    (permission_key, name_key, description_key, risk, allowed_scopes, resource_kinds, owner_condition_supported)
VALUES
    ('artifact.upload', 'i18n:PERMISSION_ARTIFACT_UPLOAD_NAME', 'i18n:PERMISSION_ARTIFACT_UPLOAD_DESCRIPTION', 'WRITE',
     ARRAY['ORGANIZATION','PROJECT'], ARRAY['ORGANIZATION','PROJECT','ARTIFACT'], false),
    ('agent.avatar.manage', 'i18n:PERMISSION_AGENT_AVATAR_MANAGE_NAME', 'i18n:PERMISSION_AGENT_AVATAR_MANAGE_DESCRIPTION', 'WRITE',
     ARRAY['ORGANIZATION','PROJECT','RESOURCE_KIND','RESOURCE_INSTANCE'], ARRAY['AGENT'], false),
    ('provider.account.view', 'i18n:PERMISSION_PROVIDER_ACCOUNT_VIEW_NAME', 'i18n:PERMISSION_PROVIDER_ACCOUNT_VIEW_DESCRIPTION', 'READ',
     ARRAY['ORGANIZATION','RESOURCE_KIND','RESOURCE_INSTANCE'], ARRAY['PROVIDER_ACCOUNT'], false),
    ('provider.account.manage', 'i18n:PERMISSION_PROVIDER_ACCOUNT_MANAGE_NAME', 'i18n:PERMISSION_PROVIDER_ACCOUNT_MANAGE_DESCRIPTION', 'ADMIN',
     ARRAY['ORGANIZATION','RESOURCE_KIND','RESOURCE_INSTANCE'], ARRAY['PROVIDER_ACCOUNT'], false),
    ('provider.account.authorize', 'i18n:PERMISSION_PROVIDER_ACCOUNT_AUTHORIZE_NAME', 'i18n:PERMISSION_PROVIDER_ACCOUNT_AUTHORIZE_DESCRIPTION', 'ADMIN',
     ARRAY['ORGANIZATION','RESOURCE_KIND','RESOURCE_INSTANCE'], ARRAY['PROVIDER_ACCOUNT'], false),
    ('provider.account.revoke', 'i18n:PERMISSION_PROVIDER_ACCOUNT_REVOKE_NAME', 'i18n:PERMISSION_PROVIDER_ACCOUNT_REVOKE_DESCRIPTION', 'ADMIN',
     ARRAY['ORGANIZATION','RESOURCE_KIND','RESOURCE_INSTANCE'], ARRAY['PROVIDER_ACCOUNT'], false),
    ('runtime.environment.disable', 'i18n:PERMISSION_RUNTIME_ENVIRONMENT_DISABLE_NAME', 'i18n:PERMISSION_RUNTIME_ENVIRONMENT_DISABLE_DESCRIPTION', 'ADMIN',
     ARRAY['ORGANIZATION','PROJECT','RESOURCE_KIND','RESOURCE_INSTANCE'], ARRAY['RUNTIME_ENVIRONMENT'], false),
    ('runtime.environment.delete', 'i18n:PERMISSION_RUNTIME_ENVIRONMENT_DELETE_NAME', 'i18n:PERMISSION_RUNTIME_ENVIRONMENT_DELETE_DESCRIPTION', 'ADMIN',
     ARRAY['ORGANIZATION','PROJECT','RESOURCE_KIND','RESOURCE_INSTANCE'], ARRAY['RUNTIME_ENVIRONMENT'], false)
ON CONFLICT (permission_key) DO NOTHING;

-- +goose StatementBegin
DO $$
DECLARE
    role_row record;
    next_version_id uuid;
    next_permissions text[];
BEGIN
    FOR role_row IN
        SELECT role.id AS role_id, role.organization_id, role.current_version_id,
               role.stable_key, version.revision, version.name, version.description,
               version.permission_keys, version.allowed_scopes, version.created_by
        FROM control_plane.application_roles role
        JOIN control_plane.application_role_versions version ON version.id = role.current_version_id
        WHERE role.stable_key IN ('OWNER', 'ADMINISTRATOR')
    LOOP
        next_permissions := ARRAY(
            SELECT DISTINCT permission_key
            FROM unnest(role_row.permission_keys || ARRAY[
                'artifact.upload', 'agent.avatar.manage', 'provider.account.view',
                'provider.account.manage', 'provider.account.authorize', 'provider.account.revoke',
                'runtime.environment.disable', 'runtime.environment.delete'
            ]) permission_key
            ORDER BY permission_key
        );

        INSERT INTO control_plane.application_role_versions
            (ref, organization_id, role_id, revision, name, description, permission_keys,
             allowed_scopes, change_comment, created_by)
        VALUES
            ('arv_mvp_' || substr(md5(role_row.role_id::text || role_row.revision::text), 1, 20),
             role_row.organization_id, role_row.role_id, role_row.revision + 1,
             role_row.name, role_row.description, next_permissions, role_row.allowed_scopes,
             'i18n:SYSTEM_ROLE_MVP_BACKEND_LIFECYCLES', role_row.created_by)
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

ALTER TABLE control_plane.provider_accounts
    DROP CONSTRAINT provider_accounts_organization_id_stable_key_key;

CREATE TABLE control_plane.provider_authorization_attempts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^pauth_[A-Za-z0-9_-]{8,88}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    provider_account_id uuid NOT NULL REFERENCES control_plane.provider_accounts(id),
    method text NOT NULL CHECK (method IN ('DEVICE_CODE', 'API_KEY')),
    state text NOT NULL CHECK (state IN ('PENDING', 'AUTHORIZED', 'EXPIRED', 'FAILED')),
    materializer_attempt_ref text NOT NULL DEFAULT '' CHECK (char_length(materializer_attempt_ref) <= 128),
    verification_uri text NOT NULL DEFAULT '' CHECK (char_length(verification_uri) <= 2000),
    user_code text NOT NULL DEFAULT '' CHECK (char_length(user_code) <= 128),
    expires_at timestamptz,
    safe_failure_code text NOT NULL DEFAULT '' CHECK (safe_failure_code = '' OR safe_failure_code ~ '^[A-Z0-9_]+$'),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_by uuid NOT NULL REFERENCES control_plane.subjects(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK (method <> 'DEVICE_CODE' OR state <> 'PENDING' OR
           (materializer_attempt_ref <> '' AND verification_uri <> '' AND user_code <> '' AND expires_at IS NOT NULL))
);
CREATE UNIQUE INDEX provider_authorization_one_pending
    ON control_plane.provider_authorization_attempts (provider_account_id)
    WHERE state = 'PENDING';
CREATE INDEX provider_accounts_search
    ON control_plane.provider_accounts (organization_id, updated_at DESC, ref);

CREATE TABLE control_plane.schedule_revisions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^srev_[A-Za-z0-9_-]{8,89}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    schedule_id uuid NOT NULL REFERENCES control_plane.schedules(id) DEFERRABLE INITIALLY DEFERRED,
    revision bigint NOT NULL CHECK (revision > 0),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 160),
    target_type text NOT NULL CHECK (target_type IN ('AGENT', 'WORKFLOW')),
    target_ref text NOT NULL,
    preset text NOT NULL CHECK (preset IN ('HOURLY', 'DAILY', 'WEEKDAYS', 'WEEKLY')),
    cron_expression text NOT NULL CHECK (char_length(cron_expression) BETWEEN 9 AND 32),
    timezone text NOT NULL CHECK (char_length(timezone) BETWEEN 1 AND 80),
    input jsonb NOT NULL CHECK (jsonb_typeof(input) = 'object' AND octet_length(input::text) <= 65536),
    session_policy text NOT NULL CHECK (session_policy IN ('NEW_EACH_RUN', 'CONTINUE_ONE')),
    notification_policy text NOT NULL CHECK (notification_policy IN ('CONTROL_CENTER_ONLY', 'CONTROL_CENTER_AND_OPTIONAL_CHANNELS')),
    digest text NOT NULL CHECK (digest ~ '^[a-f0-9]{64}$'),
    created_by uuid NOT NULL REFERENCES control_plane.subjects(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (schedule_id, revision)
);

INSERT INTO control_plane.schedule_revisions
    (ref, organization_id, schedule_id, revision, name, target_type, target_ref, preset,
     cron_expression, timezone, input, session_policy, notification_policy, digest, created_by, created_at)
SELECT 'srev_' || replace(gen_random_uuid()::text, '-', ''), organization_id, id, 1, name,
       target_type, target_ref, preset, cron_expression, timezone, input, session_policy,
       notification_policy,
       encode(digest(convert_to(concat_ws(chr(31), name, target_type, target_ref, preset,
           cron_expression, timezone, input::text, session_policy, notification_policy), 'UTF8'), 'sha256'), 'hex'),
       created_by, created_at
FROM control_plane.schedules;

ALTER TABLE control_plane.schedules
    ADD COLUMN current_revision_id uuid REFERENCES control_plane.schedule_revisions(id) DEFERRABLE INITIALLY DEFERRED,
    ADD COLUMN continue_session_id uuid REFERENCES control_plane.sessions(id);
UPDATE control_plane.schedules schedule
SET current_revision_id = revision.id
FROM control_plane.schedule_revisions revision
WHERE revision.schedule_id = schedule.id AND revision.revision = 1;
ALTER TABLE control_plane.schedules ALTER COLUMN current_revision_id SET NOT NULL;

ALTER TABLE control_plane.schedule_occurrences
    ADD COLUMN schedule_revision_id uuid REFERENCES control_plane.schedule_revisions(id);
UPDATE control_plane.schedule_occurrences occurrence
SET schedule_revision_id = schedule.current_revision_id
FROM control_plane.schedules schedule
WHERE schedule.id = occurrence.schedule_id;
ALTER TABLE control_plane.schedule_occurrences ALTER COLUMN schedule_revision_id SET NOT NULL;

CREATE TRIGGER protect_schedule_revision
BEFORE UPDATE OR DELETE ON control_plane.schedule_revisions
FOR EACH ROW EXECUTE FUNCTION control_plane.protect_immutable_row();

CREATE TABLE control_plane.role_image_recipe_revisions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^imgrev_[A-Za-z0-9_-]{8,88}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    project_id uuid NOT NULL REFERENCES control_plane.projects(id),
    recipe_id uuid NOT NULL REFERENCES control_plane.role_image_recipes(id),
    revision bigint NOT NULL CHECK (revision > 0),
    recipe_version bigint NOT NULL CHECK (recipe_version > 0),
    recipe_generation bigint NOT NULL CHECK (recipe_generation > 0),
    specification jsonb NOT NULL CHECK (jsonb_typeof(specification) = 'object'),
    spec_sha256 text NOT NULL CHECK (spec_sha256 ~ '^[a-f0-9]{64}$'),
    image_artifact_id uuid REFERENCES control_plane.image_artifacts(id),
    provenance_sha256 text NOT NULL DEFAULT '' CHECK (provenance_sha256 = '' OR provenance_sha256 ~ '^[a-f0-9]{64}$'),
    source_sha256 text NOT NULL DEFAULT '' CHECK (source_sha256 = '' OR source_sha256 ~ '^[a-f0-9]{64}$'),
    immutable_build_sha256 text NOT NULL DEFAULT '' CHECK (immutable_build_sha256 = '' OR immutable_build_sha256 ~ '^[a-f0-9]{64}$'),
    manifest_digest text NOT NULL DEFAULT '' CHECK (manifest_digest = '' OR manifest_digest ~ '^sha256:[a-f0-9]{64}$'),
    promoted_reference text NOT NULL DEFAULT '' CHECK (char_length(promoted_reference) <= 1000),
    promotion_receipt_sha256 text NOT NULL DEFAULT '' CHECK (promotion_receipt_sha256 = '' OR promotion_receipt_sha256 ~ '^[a-f0-9]{64}$'),
    created_by uuid NOT NULL REFERENCES control_plane.subjects(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (recipe_id, revision)
);

INSERT INTO control_plane.role_image_recipe_revisions
    (ref, organization_id, project_id, recipe_id, revision, recipe_version, recipe_generation,
     specification, spec_sha256, image_artifact_id, provenance_sha256, source_sha256,
     immutable_build_sha256, manifest_digest, promoted_reference, promotion_receipt_sha256,
     created_by, created_at)
SELECT 'imgrev_' || replace(gen_random_uuid()::text, '-', ''), recipe.organization_id,
       recipe.project_id, recipe.id, 1, recipe.version, recipe.generation, recipe.specification,
       recipe.spec_sha256, artifact.id, coalesce(artifact.provenance_sha256, ''),
       coalesce(artifact.specification->>'source_sha256', ''), coalesce(artifact.immutable_build_sha256, ''),
       coalesce(artifact.manifest_digest, ''), coalesce(artifact.promoted_reference, ''),
       coalesce(artifact.promotion_readback_sha256, ''), recipe.created_by, recipe.created_at
FROM control_plane.role_image_recipes recipe
LEFT JOIN control_plane.image_artifacts artifact ON artifact.id = recipe.active_image_artifact_id;

CREATE TRIGGER protect_role_image_recipe_revision
BEFORE UPDATE OR DELETE ON control_plane.role_image_recipe_revisions
FOR EACH ROW EXECUTE FUNCTION control_plane.protect_immutable_row();

CREATE TABLE control_plane.role_image_promotion_requests (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^imgprom_[A-Za-z0-9_-]{8,87}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    project_id uuid NOT NULL REFERENCES control_plane.projects(id),
    recipe_id uuid NOT NULL REFERENCES control_plane.role_image_recipes(id),
    image_artifact_id uuid NOT NULL REFERENCES control_plane.image_artifacts(id),
    expected_provenance_sha256 text NOT NULL CHECK (expected_provenance_sha256 ~ '^[a-f0-9]{64}$'),
    manifest_digest text NOT NULL CHECK (manifest_digest ~ '^sha256:[a-f0-9]{64}$'),
    receipt_sha256 text NOT NULL CHECK (receipt_sha256 ~ '^[a-f0-9]{64}$'),
    state text NOT NULL DEFAULT 'QUEUED' CHECK (state IN ('QUEUED', 'PROMOTING', 'PROMOTED', 'FAILED')),
    requested_by uuid NOT NULL REFERENCES control_plane.subjects(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (image_artifact_id)
);

ALTER TABLE control_plane.image_artifacts
    ADD COLUMN promotion_request_id uuid UNIQUE REFERENCES control_plane.role_image_promotion_requests(id);

ALTER TABLE control_plane.runtime_environment_sets
    DROP CONSTRAINT runtime_environment_sets_state_check,
    ADD CONSTRAINT runtime_environment_sets_state_check CHECK (state IN ('ACTIVE', 'DISABLED', 'DELETED'));

ALTER TABLE control_plane.integration_connections
    ADD COLUMN lifecycle_state text NOT NULL DEFAULT 'ACTIVE' CHECK (lifecycle_state IN ('ACTIVE', 'DELETED'));

ALTER TABLE control_plane.owner_gates
    ADD COLUMN source_attachment_set_id uuid REFERENCES control_plane.attachment_sets(id);

ALTER TABLE control_plane.agents
    ADD COLUMN avatar_artifact_id uuid REFERENCES control_plane.artifacts(id),
    ADD COLUMN avatar_artifact_revision bigint CHECK (avatar_artifact_revision IS NULL OR avatar_artifact_revision > 0),
    ADD CONSTRAINT agents_avatar_artifact_pair CHECK (
        (avatar_artifact_id IS NULL AND avatar_artifact_revision IS NULL) OR
        (avatar_artifact_id IS NOT NULL AND avatar_artifact_revision IS NOT NULL)
    );

CREATE INDEX runtime_environment_agents
    ON control_plane.agent_runtime_environment_bindings (environment_set_id, agent_id);
CREATE INDEX owner_gates_cursor
    ON control_plane.owner_gates (organization_id, created_at DESC, ref);
CREATE INDEX audit_events_cursor
    ON control_plane.audit_events (organization_id, occurred_at DESC, ref);
CREATE INDEX integration_definitions_search
    ON control_plane.integration_definitions (category, name, stable_key);
CREATE INDEX integration_connections_search
    ON control_plane.integration_connections (organization_id, updated_at DESC, ref)
    WHERE lifecycle_state = 'ACTIVE';

GRANT SELECT, INSERT, UPDATE ON control_plane.provider_authorization_attempts TO control_plane_runtime;
GRANT SELECT, INSERT ON control_plane.schedule_revisions TO control_plane_runtime;
GRANT SELECT, INSERT ON control_plane.role_image_recipe_revisions TO control_plane_runtime;
GRANT SELECT, INSERT, UPDATE ON control_plane.role_image_promotion_requests TO control_plane_runtime;

RESET ROLE;
