-- name: SessionBlocksRuntimeCleanup :one
-- Вызывается только после FOR UPDATE блокировки authoritative Session row.
-- Любой новый turn/continuation/owner gate также обязан блокировать Session,
-- поэтому проверка и выдача cleanup claim не имеют TOCTOU окна.
SELECT EXISTS (
    SELECT 1
    FROM control_plane.resources AS turn
    WHERE turn.organization_id = @organization_id::uuid
      AND turn.project_id = @project_id::uuid
      AND turn.kind = 'TURN'
      AND turn.spec ->> 'sessionId' = @session_id
      AND turn.state IN (
          'QUEUED', 'CLAIMED', 'RUNNING', 'WAITING_OWNER',
          'WAITING_EXTERNAL', 'BLOCKED'
      )
    UNION ALL
    SELECT 1
    FROM control_plane.runtime_executions AS execution
    WHERE execution.organization_id = @organization_id::uuid
      AND execution.project_id = @project_id::uuid
      AND execution.session_id = @session_id::uuid
      AND execution.state IN ('PENDING', 'ADMITTED', 'RUNNING')
    UNION ALL
    SELECT 1
    FROM control_plane.integration_continuations AS continuation
    WHERE continuation.organization_id = @organization_id::uuid
      AND continuation.project_id = @project_id::uuid
      AND continuation.session_id = @session_id::uuid
      AND continuation.continuation_state <> 'REJOINED'
    UNION ALL
    SELECT 1
    FROM control_plane.runtime_retention_holds AS hold
    WHERE hold.organization_id = @organization_id::uuid
      AND hold.project_id = @project_id::uuid
      AND hold.session_id = @session_id::uuid
      AND hold.state = 'ACTIVE'
    UNION ALL
    SELECT 1
    FROM control_plane.resources AS gate
    WHERE gate.organization_id = @organization_id::uuid
      AND gate.project_id = @project_id::uuid
      AND gate.kind = 'OWNER_GATE'
      AND gate.state = 'WAITING_OWNER'
      AND gate.parent_id IN (
          SELECT nullif(turn.spec ->> 'processRunId', '')::uuid
          FROM control_plane.resources AS turn
          WHERE turn.organization_id = @organization_id::uuid
            AND turn.project_id = @project_id::uuid
            AND turn.kind = 'TURN'
            AND turn.spec ->> 'sessionId' = @session_id
            AND nullif(turn.spec ->> 'processRunId', '') IS NOT NULL
      )
    UNION ALL
    SELECT 1
    FROM control_plane.scheduled_runs AS scheduled
    JOIN control_plane.schedule_occurrences AS occurrence
      ON occurrence.id = scheduled.occurrence_id
    WHERE occurrence.organization_id = @organization_id::uuid
      AND occurrence.project_id = @project_id::uuid
      AND scheduled.session_id = @session_id::uuid
      AND scheduled.state = 'CLAIMED'
);
