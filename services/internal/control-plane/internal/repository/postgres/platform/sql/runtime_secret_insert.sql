-- name: runtime_secret_insert :one
INSERT INTO control_plane.runtime_secrets
  (ref, organization_id, project_id, scope_kind, namespace, name, description, value_type, state, created_by,purpose)
VALUES
  (@ref, @organization_id::uuid, NULLIF(@project_id, '')::uuid, @scope_kind, @namespace, @name, @description, @value_type, 'PROVISIONING', @actor_id::uuid,@purpose)
RETURNING id::text, version, current_revision, created_at, updated_at;
