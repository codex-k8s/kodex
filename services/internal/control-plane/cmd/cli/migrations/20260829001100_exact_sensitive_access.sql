-- +goose Up
SET ROLE control_plane_owner;

ALTER TABLE control_plane.access_bindings
    DROP CONSTRAINT access_bindings_resource_kind_check;
ALTER TABLE control_plane.access_bindings
    ADD CONSTRAINT access_bindings_resource_kind_check
    CHECK (resource_kind IS NULL OR resource_kind IN (
        'PROJECT', 'AGENT', 'WORKFLOW', 'RUN', 'SESSION', 'OWNER_GATE',
        'ARTIFACT', 'SCHEDULE', 'INTEGRATION', 'RUNTIME_ENVIRONMENT',
        'ROLE_IMAGE', 'SECRET'
    ));

INSERT INTO control_plane.permission_registry
    (permission_key, name_key, description_key, risk, allowed_scopes, resource_kinds, owner_condition_supported)
VALUES
    ('run.cancel', 'i18n:PERMISSION_RUN_CANCEL_NAME', 'i18n:PERMISSION_RUN_CANCEL_DESCRIPTION', 'ADMIN',
     ARRAY['ORGANIZATION','PROJECT','RESOURCE_KIND','RESOURCE_INSTANCE'], ARRAY['RUN'], false),
    ('session.cancel', 'i18n:PERMISSION_SESSION_CANCEL_NAME', 'i18n:PERMISSION_SESSION_CANCEL_DESCRIPTION', 'ADMIN',
     ARRAY['ORGANIZATION','PROJECT','RESOURCE_KIND','RESOURCE_INSTANCE'], ARRAY['SESSION'], false),
    ('prompt.full.view', 'i18n:PERMISSION_PROMPT_FULL_VIEW_NAME', 'i18n:PERMISSION_PROMPT_FULL_VIEW_DESCRIPTION', 'ADMIN',
     ARRAY['ORGANIZATION','PROJECT','RESOURCE_KIND','RESOURCE_INSTANCE'], ARRAY['RUN','SESSION'], false),
    ('artifact.bind', 'i18n:PERMISSION_ARTIFACT_BIND_NAME', 'i18n:PERMISSION_ARTIFACT_BIND_DESCRIPTION', 'WRITE',
     ARRAY['ORGANIZATION','PROJECT','RESOURCE_KIND','RESOURCE_INSTANCE'], ARRAY['ARTIFACT'], false),
    ('artifact.delete', 'i18n:PERMISSION_ARTIFACT_DELETE_NAME', 'i18n:PERMISSION_ARTIFACT_DELETE_DESCRIPTION', 'WRITE',
     ARRAY['ORGANIZATION','PROJECT','RESOURCE_KIND','RESOURCE_INSTANCE'], ARRAY['ARTIFACT'], false),
    ('artifact.restore', 'i18n:PERMISSION_ARTIFACT_RESTORE_NAME', 'i18n:PERMISSION_ARTIFACT_RESTORE_DESCRIPTION', 'WRITE',
     ARRAY['ORGANIZATION','PROJECT','RESOURCE_KIND','RESOURCE_INSTANCE'], ARRAY['ARTIFACT'], false),
    ('artifact.purge', 'i18n:PERMISSION_ARTIFACT_PURGE_NAME', 'i18n:PERMISSION_ARTIFACT_PURGE_DESCRIPTION', 'ADMIN',
     ARRAY['ORGANIZATION','PROJECT','RESOURCE_KIND','RESOURCE_INSTANCE'], ARRAY['ARTIFACT'], false),
    ('image.build', 'i18n:PERMISSION_IMAGE_BUILD_NAME', 'i18n:PERMISSION_IMAGE_BUILD_DESCRIPTION', 'ADMIN',
     ARRAY['ORGANIZATION','PROJECT','RESOURCE_KIND','RESOURCE_INSTANCE'], ARRAY['PROJECT','ROLE_IMAGE'], false),
    ('image.promote', 'i18n:PERMISSION_IMAGE_PROMOTE_NAME', 'i18n:PERMISSION_IMAGE_PROMOTE_DESCRIPTION', 'ADMIN',
     ARRAY['ORGANIZATION','PROJECT','RESOURCE_KIND','RESOURCE_INSTANCE'], ARRAY['ROLE_IMAGE'], false)
ON CONFLICT (permission_key) DO NOTHING;

-- Системные роли получают только специализированные permissions. Старые
-- широкие ключи удаляются из новой immutable revision и больше не принимаются.
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
            FROM unnest(
                array_remove(array_remove(role_row.permission_keys, 'run.cancel.any'), 'artifact.manage') ||
                ARRAY['run.cancel','session.cancel','prompt.full.view','artifact.bind','artifact.delete',
                      'artifact.restore','artifact.purge','image.build','image.promote']
            ) value
            ORDER BY value
        );
        next_ref := 'arv_sensitive_' || substr(md5(role_row.role_id::text), 1, 16);
        INSERT INTO control_plane.application_role_versions
            (ref, organization_id, role_id, revision, name, description, permission_keys,
             allowed_scopes, change_comment, created_by)
        VALUES
            (next_ref, role_row.organization_id, role_row.role_id, role_row.revision + 1,
             role_row.name, role_row.description, next_permissions,
             role_row.allowed_scopes, 'i18n:SYSTEM_ROLE_EXACT_SENSITIVE_ACCESS', role_row.created_by)
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

DELETE FROM control_plane.permission_registry
WHERE permission_key IN ('run.cancel.any', 'artifact.manage');

RESET ROLE;

-- +goose Down
SET ROLE control_plane_owner;

INSERT INTO control_plane.permission_registry
    (permission_key, name_key, description_key, risk, allowed_scopes, resource_kinds, owner_condition_supported)
VALUES
    ('run.cancel.any', 'i18n:PERMISSION_RUN_CANCEL_ANY_NAME', 'i18n:PERMISSION_RUN_CANCEL_ANY_DESCRIPTION', 'ADMIN',
     ARRAY['ORGANIZATION','PROJECT','RESOURCE_KIND','RESOURCE_INSTANCE'], ARRAY['RUN'], false),
    ('artifact.manage', 'i18n:PERMISSION_ARTIFACT_MANAGE_NAME', 'i18n:PERMISSION_ARTIFACT_MANAGE_DESCRIPTION', 'WRITE',
     ARRAY['ORGANIZATION','PROJECT','RESOURCE_KIND','RESOURCE_INSTANCE'], ARRAY['ARTIFACT'], false)
ON CONFLICT (permission_key) DO NOTHING;

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
        WHERE current_version.ref LIKE 'arv_sensitive_%'
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
WHERE permission_key IN (
    'run.cancel', 'session.cancel', 'prompt.full.view', 'artifact.bind',
    'artifact.delete', 'artifact.restore', 'artifact.purge', 'image.build', 'image.promote'
);

ALTER TABLE control_plane.access_bindings
    DROP CONSTRAINT access_bindings_resource_kind_check;
ALTER TABLE control_plane.access_bindings
    ADD CONSTRAINT access_bindings_resource_kind_check
    CHECK (resource_kind IS NULL OR resource_kind IN (
        'PROJECT', 'AGENT', 'WORKFLOW', 'RUN', 'OWNER_GATE', 'ARTIFACT',
        'SCHEDULE', 'INTEGRATION', 'SECRET'
    ));

RESET ROLE;
