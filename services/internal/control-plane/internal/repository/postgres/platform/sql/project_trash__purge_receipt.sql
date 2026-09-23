-- name: project_trash__purge_receipt :exec
INSERT INTO control_plane.project_purge_receipts
    (project_id, organization_id, project_ref, deleted_by, state)
VALUES (@project_id::uuid, @organization_id::uuid, @project_ref, @actor_id::uuid, 'PENDING')
