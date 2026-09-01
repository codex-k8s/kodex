-- name: bootstrap_component_tool_call_outbox_readback :one
SELECT convert_from(outbox.payload, 'UTF8')::jsonb #>> '{data,toolCall,tool}'
FROM control_plane.outbox_events outbox
JOIN control_plane.run_events event ON event.event_id = outbox.event_id
WHERE event.ref = $1
