-- name: access_insert_role_version :one
INSERT INTO control_plane.application_role_versions
    (ref, organization_id, role_id, revision, name, description, permission_keys, allowed_scopes, change_comment, created_by)
VALUES (@ref, @organization_id::uuid, @role_id::uuid, @revision, @name, @description,
        @permission_keys::text[], @allowed_scopes::text[], @change_comment, @created_by::uuid)
RETURNING id::text, ref, revision, name, description, permission_keys, allowed_scopes, change_comment, created_at
