-- name: project_trash__cancel_occurrences :many
UPDATE control_plane.schedule_occurrences occurrence
SET state='CANCELLED', safe_error_code='PROJECT_TRASHED',
    lease_ref=NULL, fence_digest=NULL, workload_instance=NULL, lease_expires_at=NULL,
    completed_at=clock_timestamp(), version=occurrence.version+1, updated_at=clock_timestamp()
FROM control_plane.schedules schedule
WHERE schedule.id=occurrence.schedule_id
  AND schedule.organization_id=@organization_id::uuid
  AND schedule.project_id=@project_id::uuid
  AND occurrence.organization_id=schedule.organization_id
  AND occurrence.state IN ('DUE', 'CLAIMED', 'RETRY_WAIT')
RETURNING occurrence.id::text
