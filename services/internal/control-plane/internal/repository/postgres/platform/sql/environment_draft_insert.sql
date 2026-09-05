-- name: environment_draft_insert :one
INSERT INTO control_plane.runtime_environment_drafts
    (ref, organization_id, project_id, environment_ref, expected_environment_version, state, specification, created_by, base_version_id)
VALUES (@ref, @organization_id::uuid, @project_id::uuid, @environment_ref, @environment_version, 'DRAFT', @specification::jsonb, @actor_id::uuid,
    (SELECT revision.id FROM control_plane.runtime_environment_versions revision
     JOIN control_plane.runtime_environment_sets environment ON environment.id = revision.environment_set_id
     WHERE revision.organization_id = @organization_id::uuid AND environment.organization_id = @organization_id::uuid
       AND environment.project_id = @project_id::uuid AND environment.ref = @environment_ref
       AND revision.ref = @base_version_ref))
RETURNING saved_at;
