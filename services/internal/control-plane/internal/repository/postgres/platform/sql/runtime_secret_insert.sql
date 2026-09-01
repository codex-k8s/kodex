-- name: runtime_secret_insert :one
INSERT INTO control_plane.runtime_secrets
  (ref, organization_id, project_id, namespace, name, description, value_type, state, created_by)
VALUES
  (@ref, @organization_id::uuid, @project_id::uuid, @namespace, @name, @description, @value_type, 'PROVISIONING', @actor_id::uuid)
RETURNING id::text, version, current_revision, created_at, updated_at;
