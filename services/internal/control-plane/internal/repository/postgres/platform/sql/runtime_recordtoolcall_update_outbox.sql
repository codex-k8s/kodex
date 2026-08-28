-- name: runtime_recordtoolcall_update_outbox :exec
UPDATE control_plane.outbox_events outbox
SET payload=jsonb_set(outbox.payload,'{data,toolCall}',$2::jsonb,true)
WHERE outbox.organization_id=$1::uuid
  AND outbox.event_id=(SELECT event_id FROM control_plane.run_events WHERE ref=$3)
