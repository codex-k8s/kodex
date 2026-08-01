-- name: ProcessHasOpenWork
SELECT EXISTS (
    SELECT 1
    FROM control_plane.resources AS work
    WHERE work.organization_id = @organization_id::uuid
      AND work.project_id = @project_id::uuid
      AND (
          (work.kind = 'TURN'
           AND work.spec ->> 'processRunId' = @process_id
           AND (@exclude_turn_id = '' OR work.id <> @exclude_turn_id::uuid)
           AND work.state NOT IN ('SUCCEEDED', 'FAILED', 'CANCELLED', 'EXPIRED', 'DELETED'))
          OR
          (work.kind = 'OWNER_GATE'
           AND work.spec ->> 'processRunId' = @process_id
           AND (@exclude_gate_id = '' OR work.id <> @exclude_gate_id::uuid)
           AND work.state = 'WAITING_OWNER')
          OR
          (work.kind = 'PROCESS_RUN'
           AND work.spec ->> 'parentProcessRunId' = @process_id
           AND work.state NOT IN ('SUCCEEDED', 'FAILED', 'CANCELLED', 'EXPIRED', 'DELETED'))
      )
)
