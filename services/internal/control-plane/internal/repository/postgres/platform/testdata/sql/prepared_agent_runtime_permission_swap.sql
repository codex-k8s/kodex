-- Только disposable fixture: сохранить project.manage/agent.view, отозвать agent.manage.
WITH prior AS MATERIALIZED (
    SELECT binding.id, binding.role_version_id
    FROM control_plane.access_bindings binding
    JOIN control_plane.projects project ON project.id = binding.project_id
    WHERE binding.organization_id = $1::uuid AND binding.subject_id = $2::uuid
      AND project.ref = $3 AND binding.presentation_kind = 'PROJECT_MEMBERSHIP'
      AND binding.state = 'ACTIVE'
    FOR UPDATE OF binding
)
UPDATE control_plane.access_bindings binding
SET role_version_id = (SELECT version.id FROM control_plane.application_role_versions version
                      WHERE version.organization_id = $1::uuid AND version.ref = $4)
FROM prior WHERE binding.id = prior.id
RETURNING prior.role_version_id::text;
