-- name: commands_launchrun_insert_session_turns_ref_session_id_turn_number :one
INSERT INTO control_plane.session_turns(ref,organization_id,session_id,run_id,turn_number,actor_kind,actor_ref,content,artifact_refs,state) SELECT $1,$2::uuid,$3::uuid,$4::uuid,next_turn_number,'USER',$5,$6,$7,'QUEUED' FROM control_plane.sessions WHERE id=$3::uuid RETURNING id::text
