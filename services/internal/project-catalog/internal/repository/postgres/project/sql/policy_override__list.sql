-- name: policy_override__list :many
SELECT
    id,
    project_id,
    target_type,
    target_id,
    payload,
    reason,
    status,
    expires_at,
    created_by_actor_ref,
    version,
    created_at,
    updated_at
FROM project_catalog_policy_overrides
WHERE project_id = @project_id
  AND (cardinality(@target_types::text[]) = 0 OR target_type = ANY(@target_types::text[]))
  AND (@target_id::uuid IS NULL OR target_id = @target_id::uuid)
  AND (
      @active_only::boolean IS FALSE
      OR (status = 'active' AND expires_at > COALESCE(@active_at::timestamptz, now()))
  )
  AND (
      @active_only::boolean IS TRUE
      OR cardinality(@statuses::text[]) = 0
      OR status = ANY(@statuses::text[])
  )
ORDER BY created_at DESC, id
LIMIT @limit::int
OFFSET @offset::int;
