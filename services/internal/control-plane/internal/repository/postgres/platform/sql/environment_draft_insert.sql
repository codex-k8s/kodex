-- name: environment_draft_insert :exec
INSERT INTO control_plane.runtime_environment_drafts
    (ref, organization_id, project_id, environment_ref, expected_environment_version, state, specification, created_by)
VALUES (@ref, @organization_id::uuid, @project_id::uuid, @environment_ref, @environment_version, 'DRAFT', @specification::jsonb, @actor_id::uuid);
