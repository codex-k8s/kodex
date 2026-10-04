-- name: runtime_activity__terminal_tools :many
WITH latest AS (
  SELECT DISTINCT ON (event.node_ref, event.safe_delta -> 'Execution' ->> 'TurnRef',
    event.safe_delta -> 'Execution' ->> 'Attempt', event.tool_call ->> 'ref')
    event.node_ref, event.safe_delta, event.tool_call, event.actor_kind, event.actor_ref, event.actor_name
  FROM control_plane.run_events event
  WHERE event.organization_id = @organization_id::uuid AND event.root_run_id = @root_run_id::uuid
    AND event.type = 'TOOL_CALL_RECORDED' AND event.safe_delta -> 'Execution' IS NOT NULL
  ORDER BY event.node_ref, event.safe_delta -> 'Execution' ->> 'TurnRef',
    event.safe_delta -> 'Execution' ->> 'Attempt', event.tool_call ->> 'ref', event.sequence DESC
)
SELECT latest.node_ref, latest.tool_call, latest.actor_kind, latest.actor_ref, latest.actor_name
FROM latest
JOIN control_plane.run_nodes node ON node.ref = latest.node_ref AND node.organization_id = @organization_id::uuid
JOIN control_plane.session_turns turn ON turn.id = node.turn_id
WHERE latest.tool_call ->> 'state' = 'RUNNING'
  AND node.state IN ('COMPLETED', 'FAILED', 'CANCELLED')
  AND latest.safe_delta -> 'Execution' ->> 'TurnRef' = turn.ref
  AND (latest.safe_delta -> 'Execution' ->> 'Attempt')::integer = node.attempt
ORDER BY latest.node_ref, latest.tool_call ->> 'ref'
