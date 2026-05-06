-- name: policy_edit_proposal__get_by_id :one
SELECT
    id,
    project_id,
    repository_id,
    source_path,
    requested_changes,
    status,
    created_at
FROM project_catalog_policy_edit_proposals
WHERE id = @id;
