-- +goose Up
SET ROLE control_plane_owner;
WITH previous AS MATERIALIZED (
    SELECT role.id AS owner_role_id, revision.*
    FROM control_plane.application_roles role
    JOIN control_plane.application_role_versions revision ON revision.id=role.current_version_id
    WHERE role.kind='SYSTEM' AND role.state='ACTIVE' AND role.stable_key IN ('OWNER','ADMINISTRATOR')
      AND NOT ('platform.stt.use'=ANY(revision.permission_keys))
    FOR UPDATE OF role
), inserted AS (
    INSERT INTO control_plane.application_role_versions
        (ref,organization_id,role_id,revision,name,description,permission_keys,allowed_scopes,change_comment,created_by)
    SELECT 'arv_stt_1046_' || substr(md5(owner_role_id::text),1,24),organization_id,owner_role_id,revision+1,name,description,
           ARRAY(SELECT DISTINCT permission FROM unnest(permission_keys || ARRAY['platform.stt.use']) permission ORDER BY permission),
           allowed_scopes,'i18n:SYSTEM_ROLE_STT_PERMISSION',created_by
    FROM previous RETURNING id,role_id,organization_id
), bindings AS (
    UPDATE control_plane.access_bindings binding
    SET role_version_id=inserted.id,version=binding.version+1,updated_at=clock_timestamp()
    FROM inserted JOIN previous ON previous.owner_role_id=inserted.role_id
    WHERE binding.organization_id=inserted.organization_id AND binding.role_version_id=previous.id AND binding.state='ACTIVE'
)
UPDATE control_plane.application_roles role
SET current_version_id=inserted.id,version=role.version+1,updated_at=clock_timestamp()
FROM inserted WHERE role.id=inserted.role_id;
RESET ROLE;
