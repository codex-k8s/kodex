-- name: ScheduleOccurrenceHasOpen
SELECT EXISTS (
    SELECT 1
    FROM control_plane.schedule_occurrences
    WHERE organization_id = @organization_id::uuid
      AND project_id = @project_id::uuid
      AND schedule_id = @schedule_id::uuid
      AND state IN ('QUEUED', 'CLAIMED', 'WAITING_OWNER', 'CONTINUATION')
)
