-- name: ScheduleOccurrenceHasOpen
SELECT EXISTS (
    SELECT 1
    FROM control_plane.schedule_occurrences AS occurrence
    LEFT JOIN control_plane.scheduled_runs AS run
      ON run.occurrence_id = occurrence.id
    WHERE occurrence.organization_id = @organization_id::uuid
      AND occurrence.project_id = @project_id::uuid
      AND occurrence.schedule_id = @schedule_id::uuid
      AND (
        occurrence.state IN ('QUEUED', 'CLAIMED', 'WAITING_OWNER', 'CONTINUATION')
        OR run.state IN ('CLAIMED', 'WAITING_OWNER', 'CONTINUATION')
      )
)
