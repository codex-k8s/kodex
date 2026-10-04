-- name: runtime_activity__latest :one
SELECT event.sequence, event.safe_delta, event.tool_call
FROM control_plane.run_events event
WHERE event.organization_id = @organization_id::uuid
  AND event.root_run_id = @root_run_id::uuid
  AND event.node_ref = @node_ref
  AND event.safe_delta -> 'Execution' ->> 'TurnRef' = @turn_ref
  AND (event.safe_delta -> 'Execution' ->> 'Attempt')::integer = @attempt
  AND ((@activity_kind = 'MESSAGE' AND event.safe_delta -> 'Message' ->> 'Ref' = @activity_ref)
    OR (@activity_kind = 'TOOL' AND event.tool_call ->> 'ref' = @activity_ref))
ORDER BY event.sequence DESC LIMIT 1
