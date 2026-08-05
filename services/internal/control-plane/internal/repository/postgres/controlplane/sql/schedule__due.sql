SELECT
    id::text,
    organization_id::text,
    project_id::text,
    coalesce(parent_id::text, ''),
    owner_actor_id::text,
    kind,
    name,
    state,
    version,
    spec,
    created_at,
    updated_at
FROM control_plane.resources
WHERE organization_id = @organization_id::uuid
  AND project_id = @project_id::uuid
  AND kind = 'SCHEDULE'
  AND state = 'ACTIVE'
  AND schedule_next_run_at <= @now
  AND NOT (
      spec->>'overlapPolicy' = 'FORBID'
      AND EXISTS (
          SELECT 1
          FROM control_plane.schedule_occurrences AS open_occurrence
          WHERE open_occurrence.organization_id = resources.organization_id
            AND open_occurrence.project_id = resources.project_id
            AND open_occurrence.schedule_id = resources.id
            AND open_occurrence.state = ANY(ARRAY['CLAIMED','WAITING_OWNER','CONTINUATION']::text[])
      )
      OR spec->>'overlapPolicy' = 'FORBID'
      AND EXISTS (
          SELECT 1
          FROM control_plane.schedule_occurrences AS run_owner
          JOIN control_plane.scheduled_runs AS open_run
            ON open_run.occurrence_id = run_owner.id
           AND open_run.state = ANY(ARRAY['CLAIMED','WAITING_OWNER','CONTINUATION']::text[])
          WHERE run_owner.organization_id = resources.organization_id
            AND run_owner.project_id = resources.project_id
            AND run_owner.schedule_id = resources.id
      )
  )
ORDER BY schedule_next_run_at, id
FOR UPDATE SKIP LOCKED
LIMIT @limit
