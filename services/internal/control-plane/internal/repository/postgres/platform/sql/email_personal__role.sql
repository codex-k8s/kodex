-- name: email_personal__role :one
SELECT role.id::text,role.ref,role.kind,role.state,version.id::text,version.revision,
       version.permission_keys,version.allowed_scopes
FROM control_plane.application_roles role
JOIN control_plane.application_role_versions version ON version.id=role.current_version_id
  AND version.role_id=role.id AND version.organization_id=role.organization_id
WHERE role.organization_id=@organization_id::uuid AND role.stable_key='PERSONAL_EMAIL_MAILBOX_METADATA'
FOR SHARE OF role;
