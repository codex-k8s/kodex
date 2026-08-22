-- name: platform__commands_emitrunevent_4 :exec
INSERT INTO control_plane.outbox_events(event_id,subject,ordering_key,sequence,payload) VALUES($1,$2,$3,$4,$5)
