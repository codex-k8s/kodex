-- name: SessionOpenTurns
SELECT
    turn.id::text,
    turn.organization_id::text,
    turn.project_id::text,
    coalesce(turn.parent_id::text, ''),
    turn.owner_actor_id::text,
    turn.kind,
    turn.name,
    turn.state,
    turn.version,
    turn.spec,
    turn.created_at,
    turn.updated_at,
    coalesce(lease.turn_id::text, ''),
    coalesce(lease.token_hash, ''),
    coalesce(lease.workload_id, ''),
    coalesce(lease.authority_generation, 0),
    coalesce(lease.attempt, 0),
    coalesce(lease.expires_at, 'epoch'::timestamptz),
    coalesce(lease.fence, 0),
    attempt.turn_id::text,
    attempt.attempt,
    attempt.workload_id,
    attempt.authority_generation,
    attempt.state,
    attempt.input_sha256,
    attempt.lease_fence,
    attempt.started_at,
    coalesce(attempt.finished_at, 'epoch'::timestamptz),
    coalesce(attempt.outcome, '')
FROM control_plane.resources AS turn
LEFT JOIN control_plane.turn_leases AS lease ON lease.turn_id = turn.id
JOIN control_plane.turn_attempts AS attempt
  ON attempt.turn_id = turn.id
 AND attempt.attempt = (turn.spec ->> 'attempt')::integer
WHERE turn.organization_id = @organization_id::uuid
  AND turn.project_id = @project_id::uuid
  AND turn.kind = 'TURN'
  AND turn.spec ->> 'sessionId' = @session_id
  AND turn.state IN (
      'QUEUED', 'CLAIMED', 'RUNNING', 'WAITING_OWNER',
      'WAITING_EXTERNAL', 'BLOCKED'
  )
ORDER BY (turn.spec ->> 'sequence')::bigint, turn.id
FOR UPDATE OF turn, attempt
