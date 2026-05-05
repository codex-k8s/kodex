-- name: policy_edit_proposal__create :exec
INSERT INTO project_catalog_policy_edit_proposals (
    id, project_id, repository_id, source_path, requested_changes, status, created_at
) VALUES (
    @id, @project_id, @repository_id, @source_path, @requested_changes, @status, @created_at
);
