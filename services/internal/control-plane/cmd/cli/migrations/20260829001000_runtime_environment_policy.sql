-- +goose Up
SET ROLE control_plane_owner;

INSERT INTO control_plane.permission_registry
    (permission_key, name_key, description_key, risk, allowed_scopes, resource_kinds, owner_condition_supported)
VALUES
    ('environment.privileged.manage', 'i18n:PERMISSION_ENVIRONMENT_PRIVILEGED_MANAGE_NAME',
     'i18n:PERMISSION_ENVIRONMENT_PRIVILEGED_MANAGE_DESCRIPTION', 'ADMIN',
     ARRAY['ORGANIZATION','PROJECT','RESOURCE_KIND','RESOURCE_INSTANCE'], ARRAY['PROJECT','RUNTIME_ENVIRONMENT'], false)
ON CONFLICT (permission_key) DO NOTHING;

-- Системные owner/admin bindings переходят на новую immutable role version.
-- Пользовательские роли получают право только явной настройкой владельца.
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
        WHERE application_role.stable_key IN ('OWNER', 'ADMINISTRATOR')
    LOOP
        next_permissions := ARRAY(
            SELECT DISTINCT value
            FROM unnest(role_row.permission_keys || ARRAY['environment.privileged.manage']) value
            ORDER BY value
        );
        next_ref := 'arv_runtime_policy_' || substr(md5(role_row.role_id::text), 1, 16);
        INSERT INTO control_plane.application_role_versions
            (ref, organization_id, role_id, revision, name, description, permission_keys,
             allowed_scopes, change_comment, created_by)
        VALUES
            (next_ref, role_row.organization_id, role_row.role_id, role_row.revision + 1,
             role_row.name, role_row.description, next_permissions,
             role_row.allowed_scopes, 'i18n:SYSTEM_ROLE_RUNTIME_POLICY', role_row.created_by)
        RETURNING id INTO next_version_id;

        UPDATE control_plane.application_roles
        SET current_version_id = next_version_id,
            version = version + 1,
            updated_at = clock_timestamp()
        WHERE id = role_row.role_id;

        UPDATE control_plane.access_bindings
        SET role_version_id = next_version_id,
            version = version + 1,
            updated_at = clock_timestamp()
        WHERE role_version_id = role_row.current_version_id
          AND state = 'ACTIVE';
    END LOOP;
END $$;
-- +goose StatementEnd

ALTER TABLE control_plane.runtime_environment_versions
    ADD COLUMN core_digest text,
    ADD COLUMN resource_policy jsonb,
    ADD COLUMN volume_policy jsonb,
    ADD COLUMN network_policy jsonb,
    ADD COLUMN kubernetes_access_profile jsonb,
    ADD COLUMN resources_digest text,
    ADD COLUMN volumes_digest text,
    ADD COLUMN network_digest text,
    ADD COLUMN rbac_digest text;

ALTER TABLE control_plane.runtime_environment_versions
    DISABLE TRIGGER protect_runtime_environment_version;

UPDATE control_plane.runtime_environment_versions
SET core_digest = digest,
    resource_policy = '{"cpu_request_milli":2000,"cpu_limit_milli":2000,"memory_request_mib":4096,"memory_limit_mib":4096,"ephemeral_storage_request_mib":1024,"ephemeral_storage_limit_mib":4096}'::jsonb,
    volume_policy = '[]'::jsonb,
    network_policy = '{"deny_by_default":true,"egress":[{"destination":"DNS","protocol":"TCP","port":53},{"destination":"DNS","protocol":"UDP","port":53},{"destination":"PROVIDER_PROXY","protocol":"TCP","port":8080},{"destination":"RUNTIME_CALLBACK","protocol":"TCP","port":8444}]}'::jsonb,
    kubernetes_access_profile = '{"kind":"NONE","namespace":"kodex-runtime"}'::jsonb,
    resources_digest = '5cc6a50ed6896a06cea6acf2149f22c3d37fb52b6ef75f6305375d1bdde483b6',
    volumes_digest = 'd21f6a8d7834847c8c5e02c88ff36a1114ecbfdf43f0c6668fceff28334b3634',
    network_digest = '5572212990fe694f077ccac5b0b4ae4e4b979f812e061dd2daa8e0a66c46076b',
    rbac_digest = '1b72c0704ea77cc1536f61d4e882306f12967fd39425fa6ea8d3f68500461d2e';

UPDATE control_plane.runtime_environment_versions
SET digest = encode(digest(
    convert_to('runtime-environment-v2', 'UTF8') || decode('00', 'hex') ||
    convert_to(core_digest, 'UTF8') || decode('00', 'hex') ||
    convert_to(resources_digest, 'UTF8') || decode('00', 'hex') ||
    convert_to(volumes_digest, 'UTF8') || decode('00', 'hex') ||
    convert_to(network_digest, 'UTF8') || decode('00', 'hex') ||
    convert_to(rbac_digest, 'UTF8') || decode('00', 'hex'),
    'sha256'), 'hex');

ALTER TABLE control_plane.runtime_environment_versions
    ALTER COLUMN core_digest SET NOT NULL,
    ALTER COLUMN resource_policy SET NOT NULL,
    ALTER COLUMN volume_policy SET NOT NULL,
    ALTER COLUMN network_policy SET NOT NULL,
    ALTER COLUMN kubernetes_access_profile SET NOT NULL,
    ALTER COLUMN resources_digest SET NOT NULL,
    ALTER COLUMN volumes_digest SET NOT NULL,
    ALTER COLUMN network_digest SET NOT NULL,
    ALTER COLUMN rbac_digest SET NOT NULL,
    ADD CONSTRAINT runtime_environment_versions_core_digest_check CHECK (core_digest ~ '^[a-f0-9]{64}$'),
    ADD CONSTRAINT runtime_environment_versions_resource_policy_check CHECK (jsonb_typeof(resource_policy) = 'object'),
    ADD CONSTRAINT runtime_environment_versions_volume_policy_check CHECK (jsonb_typeof(volume_policy) = 'array' AND jsonb_array_length(volume_policy) <= 16),
    ADD CONSTRAINT runtime_environment_versions_network_policy_check CHECK (jsonb_typeof(network_policy) = 'object'),
    ADD CONSTRAINT runtime_environment_versions_kubernetes_access_profile_check CHECK (jsonb_typeof(kubernetes_access_profile) = 'object'),
    ADD CONSTRAINT runtime_environment_versions_resources_digest_check CHECK (resources_digest ~ '^[a-f0-9]{64}$'),
    ADD CONSTRAINT runtime_environment_versions_volumes_digest_check CHECK (volumes_digest ~ '^[a-f0-9]{64}$'),
    ADD CONSTRAINT runtime_environment_versions_network_digest_check CHECK (network_digest ~ '^[a-f0-9]{64}$'),
    ADD CONSTRAINT runtime_environment_versions_rbac_digest_check CHECK (rbac_digest ~ '^[a-f0-9]{64}$');

ALTER TABLE control_plane.runtime_revisions
    ADD COLUMN runtime_resources_digest text,
    ADD COLUMN runtime_volumes_digest text,
    ADD COLUMN runtime_network_digest text,
    ADD COLUMN runtime_rbac_profile_digest text,
    ADD COLUMN effective_kubernetes_access_digest text,
    ADD CONSTRAINT runtime_revisions_environment_policy_digests_check CHECK (
        (runtime_resources_digest IS NULL AND runtime_volumes_digest IS NULL AND runtime_network_digest IS NULL
         AND runtime_rbac_profile_digest IS NULL AND effective_kubernetes_access_digest IS NULL)
        OR
        (runtime_resources_digest ~ '^[a-f0-9]{64}$' AND runtime_volumes_digest ~ '^[a-f0-9]{64}$'
         AND runtime_network_digest ~ '^[a-f0-9]{64}$' AND runtime_rbac_profile_digest ~ '^[a-f0-9]{64}$'
         AND effective_kubernetes_access_digest ~ '^[a-f0-9]{64}$')
    );

ALTER TABLE control_plane.runtime_environment_versions
    ENABLE TRIGGER protect_runtime_environment_version;

RESET ROLE;

-- +goose Down
SET ROLE control_plane_owner;

ALTER TABLE control_plane.runtime_revisions
    DROP CONSTRAINT runtime_revisions_environment_policy_digests_check,
    DROP COLUMN effective_kubernetes_access_digest,
    DROP COLUMN runtime_rbac_profile_digest,
    DROP COLUMN runtime_network_digest,
    DROP COLUMN runtime_volumes_digest,
    DROP COLUMN runtime_resources_digest;

ALTER TABLE control_plane.runtime_environment_versions
    DISABLE TRIGGER protect_runtime_environment_version;

UPDATE control_plane.runtime_environment_versions
SET digest = core_digest;

ALTER TABLE control_plane.runtime_environment_versions
    DROP CONSTRAINT runtime_environment_versions_rbac_digest_check,
    DROP CONSTRAINT runtime_environment_versions_network_digest_check,
    DROP CONSTRAINT runtime_environment_versions_volumes_digest_check,
    DROP CONSTRAINT runtime_environment_versions_resources_digest_check,
    DROP CONSTRAINT runtime_environment_versions_kubernetes_access_profile_check,
    DROP CONSTRAINT runtime_environment_versions_network_policy_check,
    DROP CONSTRAINT runtime_environment_versions_volume_policy_check,
    DROP CONSTRAINT runtime_environment_versions_resource_policy_check,
    DROP CONSTRAINT runtime_environment_versions_core_digest_check,
    DROP COLUMN rbac_digest,
    DROP COLUMN network_digest,
    DROP COLUMN volumes_digest,
    DROP COLUMN resources_digest,
    DROP COLUMN kubernetes_access_profile,
    DROP COLUMN network_policy,
    DROP COLUMN volume_policy,
    DROP COLUMN resource_policy,
    DROP COLUMN core_digest;

ALTER TABLE control_plane.runtime_environment_versions
    ENABLE TRIGGER protect_runtime_environment_version;

-- +goose StatementBegin
DO $$
DECLARE
    role_row record;
    previous_version_id uuid;
BEGIN
    FOR role_row IN
        SELECT application_role.id AS role_id,
               application_role.current_version_id,
               current_version.revision
        FROM control_plane.application_roles application_role
        JOIN control_plane.application_role_versions current_version
          ON current_version.id = application_role.current_version_id
        WHERE current_version.ref LIKE 'arv_runtime_policy_%'
    LOOP
        SELECT id INTO previous_version_id
        FROM control_plane.application_role_versions
        WHERE role_id = role_row.role_id
          AND revision = role_row.revision - 1;

        IF previous_version_id IS NOT NULL THEN
            UPDATE control_plane.access_bindings
            SET role_version_id = previous_version_id,
                version = version + 1,
                updated_at = clock_timestamp()
            WHERE role_version_id = role_row.current_version_id
              AND state = 'ACTIVE';

            UPDATE control_plane.application_roles
            SET current_version_id = previous_version_id,
                version = version + 1,
                updated_at = clock_timestamp()
            WHERE id = role_row.role_id;

            DELETE FROM control_plane.application_role_versions
            WHERE id = role_row.current_version_id;
        END IF;
    END LOOP;
END $$;
-- +goose StatementEnd

DELETE FROM control_plane.permission_registry
WHERE permission_key = 'environment.privileged.manage';

RESET ROLE;
