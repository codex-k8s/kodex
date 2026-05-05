-- name: release_line__create :exec
INSERT INTO project_catalog_release_lines (
    id, project_id, release_policy_id, name, branch_pattern, status,
    version, created_at, updated_at
) VALUES (
    @id, @project_id, @release_policy_id, @name, @branch_pattern, @status,
    @version, @created_at, @updated_at
);
