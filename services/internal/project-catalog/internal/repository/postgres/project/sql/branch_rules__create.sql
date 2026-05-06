-- name: branch_rules__create :exec
INSERT INTO project_catalog_branch_rules (
    id, project_id, repository_id, pattern, required_checks, merge_policy,
    status, version, created_at, updated_at
) VALUES (
    @id, @project_id, @repository_id, @pattern, @required_checks, @merge_policy,
    @status, @version, @created_at, @updated_at
);
