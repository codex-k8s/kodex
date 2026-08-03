-- name: ScheduleOccurrenceHasBlockingExecution
SELECT EXISTS (
    SELECT 1
    FROM control_plane.schedule_occurrences AS owned_occurrence
    LEFT JOIN control_plane.scheduled_runs AS open_run
      ON open_run.occurrence_id = owned_occurrence.id
     AND open_run.state = ANY(@open_execution_states::text[])
    WHERE owned_occurrence.organization_id = @organization_id::uuid
      AND owned_occurrence.project_id = @project_id::uuid
      AND owned_occurrence.schedule_id = @schedule_id::uuid
      AND (
        (
          owned_occurrence.id <> @candidate_occurrence_id::uuid
          AND owned_occurrence.state = ANY(@open_execution_states::text[])
        )
        OR open_run.occurrence_id IS NOT NULL
      )
)
