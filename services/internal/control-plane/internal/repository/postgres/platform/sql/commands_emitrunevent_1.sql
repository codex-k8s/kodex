-- name: platform__commands_emitrunevent_1 :one
UPDATE control_plane.runs SET event_sequence=event_sequence+1,graph_revision=graph_revision+1,updated_at=clock_timestamp() WHERE id=$1::uuid RETURNING ref,event_sequence,version
