-- name: environment_draft_get :one
SELECT draft.ref, project.ref, draft.environment_ref, draft.expected_environment_version,
       draft.state, draft.version, draft.specification, draft.validation_digest, draft.diagnostics, draft.published_environment_ref
FROM control_plane.runtime_environment_drafts draft
JOIN control_plane.projects project ON project.id = draft.project_id AND project.organization_id = draft.organization_id
WHERE draft.organization_id = $1::uuid AND draft.ref = $2;
