WITH workspace_delivery_lock AS (
    SELECT pg_advisory_xact_lock(hashtextextended(
        @organization_id::text || ':' || @project_id::text, 0
    ))
)
SELECT work.id::text
FROM control_plane.interaction_delivery_work AS work
CROSS JOIN workspace_delivery_lock
WHERE work.organization_id = @organization_id::uuid
  AND work.project_id = @project_id::uuid
  AND state IN ('PENDING', 'CLAIMED')
ORDER BY work.id
FOR UPDATE;
