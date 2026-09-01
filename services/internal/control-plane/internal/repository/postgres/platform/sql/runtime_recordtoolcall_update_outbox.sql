-- name: runtime_recordtoolcall_update_outbox :exec
UPDATE control_plane.outbox_events outbox
SET payload=convert_to(
  jsonb_set(convert_from(outbox.payload,'UTF8')::jsonb,'{data,toolCall}',$2::jsonb,true)::text,
  'UTF8'
)
WHERE outbox.event_id=(
  SELECT event_id
  FROM control_plane.run_events
  WHERE organization_id=$1::uuid AND ref=$3
)
