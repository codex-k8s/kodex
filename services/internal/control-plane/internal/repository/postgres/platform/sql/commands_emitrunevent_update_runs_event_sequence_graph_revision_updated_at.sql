-- name: commands_emitrunevent_update_runs_event_sequence_graph_revision_updated_at :one
UPDATE control_plane.runs SET event_sequence=event_sequence+1,graph_revision=graph_revision+1,updated_at=clock_timestamp() WHERE id=$1::uuid RETURNING ref,event_sequence,version
