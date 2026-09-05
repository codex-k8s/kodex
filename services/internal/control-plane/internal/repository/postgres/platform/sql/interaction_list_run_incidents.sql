-- name: interaction_list_run_incidents :many
SELECT
    delivery.ref,
    project.ref,
    root.ref,
    delivery.state,
    delivery.attempt,delivery.execution_max_attempts,
    delivery.created_at
FROM control_plane.runs requested
JOIN control_plane.runs root
  ON root.id = requested.root_run_id
JOIN control_plane.projects project
  ON project.id = requested.project_id
JOIN control_plane.interaction_deliveries delivery
  ON delivery.root_run_id = root.id
 AND delivery.organization_id = requested.organization_id
WHERE requested.organization_id = @organization_id::uuid
  AND requested.ref = @run_ref
  AND (
      delivery.state IN ('FAILED','UNKNOWN_OUTCOME')
      OR (delivery.state = 'SUCCEEDED' AND delivery.attempt > 1)
  )
  AND (
      @platform_role IN ('OWNER', 'ADMINISTRATOR')
      OR EXISTS (
          SELECT 1
          FROM control_plane.memberships membership
          WHERE membership.project_id = requested.project_id
            AND membership.subject_id = @actor_id::uuid
            AND membership.active
            AND 'VIEW' = ANY(membership.permissions)
      )
  )
ORDER BY delivery.updated_at DESC
LIMIT 100
