-- name: commands_changerun_select_runs_id :one
SELECT r.target_type,r.target_ref,r.title,r.task,s.ref,r.source,r.input,r.input_artifact_refs FROM control_plane.runs r JOIN control_plane.sessions s ON s.id=r.session_id WHERE r.id=$1::uuid
