-- name: runtime_environment_effective_version_matrix :one
SELECT (%s)::text
FROM (SELECT $1::uuid AS project_id, $2::text AS system_key, $3::uuid AS current_version_id) agent
CROSS JOIN (SELECT $4::uuid AS environment_version_id) binding;
