-- name: RuntimeAgentBindingResolveIntent :one
WITH matched AS (
    SELECT turn.*, count(*) OVER () AS match_count
    FROM control_plane.resources AS turn
    WHERE turn.organization_id = @organization_id::uuid
      AND turn.project_id = @project_id::uuid
      AND turn.owner_actor_id = @actor_id::uuid
      AND turn.kind = 'TURN'
      AND turn.state = 'QUEUED'
      AND turn.spec ->> 'sourceRef' = @source_ref
)
SELECT
    session.id::text, session.organization_id::text, session.project_id::text,
    coalesce(session.parent_id::text, ''), session.owner_actor_id::text,
    session.kind, session.name, session.state, session.version, session.spec,
    session.created_at, session.updated_at,
    turn.id::text, turn.organization_id::text, turn.project_id::text,
    coalesce(turn.parent_id::text, ''), turn.owner_actor_id::text,
    turn.kind, turn.name, turn.state, turn.version, turn.spec,
    turn.created_at, turn.updated_at,
    revision.id::text, revision.organization_id::text, revision.project_id::text,
    coalesce(revision.parent_id::text, ''), revision.owner_actor_id::text,
    revision.kind, revision.name, revision.state, revision.version, revision.spec,
    revision.created_at, revision.updated_at,
    turn.match_count
FROM matched AS turn
JOIN control_plane.resources AS session
  ON session.id = (turn.spec ->> 'sessionId')::uuid
 AND session.organization_id = turn.organization_id
 AND session.project_id = turn.project_id
JOIN control_plane.resources AS revision
  ON revision.id = (turn.spec ->> 'runtimeRevisionId')::uuid
 AND revision.organization_id = turn.organization_id
 AND revision.project_id = turn.project_id
ORDER BY turn.id
LIMIT 1
FOR SHARE OF session, revision;
