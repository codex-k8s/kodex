-- name: interaction_list_failed_incidents :many
SELECT
    delivery.ref,
    project.ref,
    run.ref,
    delivery.safe_error_code,
    delivery.created_at
FROM control_plane.interaction_deliveries delivery
JOIN control_plane.projects project ON project.id = delivery.project_id
JOIN control_plane.runs run ON run.id = delivery.root_run_id
WHERE delivery.organization_id = @organization_id::uuid
  AND delivery.state = 'FAILED'
ORDER BY delivery.updated_at DESC
LIMIT 100
