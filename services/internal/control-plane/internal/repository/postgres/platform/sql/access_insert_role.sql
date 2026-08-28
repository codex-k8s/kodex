-- name: access_insert_role :one
INSERT INTO control_plane.application_roles
    (ref, organization_id, stable_key, kind, created_by)
VALUES (@ref, @organization_id::uuid, NULLIF(@stable_key, ''), @kind, @created_by::uuid)
RETURNING id::text, ref, kind, state, version, updated_at
