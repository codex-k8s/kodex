-- name: ScheduleOtherSessionReferencesForUpdate
SELECT id::text
FROM control_plane.resources
WHERE organization_id = @organization_id::uuid
  AND project_id = @project_id::uuid
  AND id <> @schedule_id::uuid
  AND kind = 'SCHEDULE'
  AND state IN ('ACTIVE', 'PAUSED')
  AND spec ->> 'executionSessionId' = @session_id
ORDER BY id
FOR UPDATE;
