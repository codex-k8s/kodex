-- name: commands_resolvegate_insert_change_request_turn :one
INSERT INTO control_plane.session_turns(
    ref,organization_id,session_id,run_id,turn_number,actor_kind,actor_ref,content,state
)
SELECT $1,$2::uuid,$3::uuid,$4::uuid,next_turn_number,'USER',$5,$6,'QUEUED'
FROM control_plane.sessions
WHERE id=$3::uuid
RETURNING id::text
