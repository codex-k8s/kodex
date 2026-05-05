-- name: release_policy__list :many
SELECT
    id, project_id, name, branch_pattern, rollout_strategy, rollback_policy,
    risk_profile_ref, status, version, created_at, updated_at
FROM project_catalog_release_policies
WHERE project_id = @project_id
  AND (cardinality(@statuses::text[]) = 0 OR status = ANY(@statuses::text[]))
ORDER BY name, id
LIMIT @limit::integer OFFSET @offset::integer;
