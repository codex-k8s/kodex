-- name: ordinary_integration_outcomes__task_pin :one
-- Приватное чтение ссылки; выдача результата требует полной проверки доступности источника.
SELECT revision.safe_snapshot->>'task'
FROM control_plane.runtime_leases lease
JOIN control_plane.runtime_revisions revision ON revision.id=lease.runtime_revision_id
 AND revision.organization_id=lease.organization_id AND revision.run_id=lease.run_id AND revision.node_id=lease.node_id
 AND revision.generation=lease.generation
WHERE lease.organization_id=@organization_id::uuid AND lease.ref=@lease_ref
 AND lease.fence_digest=@fence_digest AND lease.generation=@generation
 AND lease.state='CLAIMED' AND lease.expires_at>clock_timestamp()
 AND control_plane.runtime_execution_before_deadline(lease.organization_id,lease.run_id);
