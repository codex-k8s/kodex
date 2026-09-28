-- name: commands_resolvegate_count_open_gates :one
SELECT count(*) FROM control_plane.owner_gates
WHERE organization_id=@organization_id::uuid
  AND root_run_id=@root_run_id::uuid
  AND state='OPEN'
