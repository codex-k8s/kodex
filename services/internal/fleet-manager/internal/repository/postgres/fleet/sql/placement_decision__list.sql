-- name: placement_decision__list :many
SELECT
    id, command_id, request_fingerprint, status, fleet_scope_id, cluster_id,
    project_id, repository_id, runtime_mode, runtime_profile, input_json,
    reason_code, reason_message, used_default_path, created_at
FROM fleet_manager_placement_decisions
WHERE (@project_id::uuid IS NULL OR project_id = @project_id::uuid)
  AND (@repository_id::uuid IS NULL OR repository_id = @repository_id::uuid)
  AND (@fleet_scope_id::uuid IS NULL OR fleet_scope_id = @fleet_scope_id::uuid)
  AND (@cluster_id::uuid IS NULL OR cluster_id = @cluster_id::uuid)
  AND (cardinality(@statuses::text[]) = 0 OR status = ANY(@statuses::text[]))
ORDER BY created_at DESC, id DESC
LIMIT @limit::bigint
OFFSET @offset::bigint;
