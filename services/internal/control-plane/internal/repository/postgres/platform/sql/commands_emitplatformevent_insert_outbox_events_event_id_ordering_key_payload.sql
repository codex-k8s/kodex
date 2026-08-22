-- name: platform__commands_emitplatformevent_insert_outbox_events_event_id_ordering_key_payload :exec
INSERT INTO control_plane.outbox_events(event_id,subject,ordering_key,sequence,payload) VALUES($1,$2,$3,$4,$5)
