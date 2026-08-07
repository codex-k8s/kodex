-- name: ProviderBindingActiveSessions
WITH pinned_sessions AS (
    SELECT session.id AS session_id
    FROM control_plane.resources AS session
    WHERE session.organization_id = @organization_id::uuid
      AND session.project_id = @project_id::uuid
      AND session.kind = 'SESSION'
      AND session.state NOT IN ('ARCHIVED', 'CANCELLED', 'DELETION_PENDING', 'DELETED')
      AND session.spec ->> 'providerAccountBindingId' = @binding_id
    UNION
    SELECT execution.session_id
    FROM control_plane.runtime_executions AS execution
    WHERE execution.organization_id = @organization_id::uuid
      AND execution.project_id = @project_id::uuid
      AND execution.provider_binding_id = @binding_id::uuid
      AND execution.state IN ('PENDING', 'ADMITTED', 'RUNNING')
    UNION
    SELECT (turn.spec ->> 'sessionId')::uuid
    FROM control_plane.resources AS turn
    JOIN control_plane.resources AS revision
      ON revision.organization_id = turn.organization_id
     AND revision.project_id = turn.project_id
     AND revision.id = (turn.spec ->> 'runtimeRevisionId')::uuid
     AND revision.kind = 'RUNTIME_REVISION'
     AND revision.state <> 'DELETED'
    WHERE turn.organization_id = @organization_id::uuid
      AND turn.project_id = @project_id::uuid
      AND turn.kind = 'TURN'
      AND turn.state NOT IN ('SUCCEEDED', 'FAILED', 'CANCELLED', 'EXPIRED', 'ARCHIVED', 'DELETED')
      AND revision.spec ->> 'providerCredentialBindingId' = @binding_id
)
SELECT count(*)::bigint
FROM pinned_sessions
