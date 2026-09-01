-- name: commands_changerun_select_runs_id :one
SELECT r.target_type,r.target_ref,r.title,r.task,s.ref,r.source,r.input,
       COALESCE(attachment_set.ref,''),COALESCE(attachment_set.purpose,'')
FROM control_plane.runs r
JOIN control_plane.sessions s ON s.id=r.session_id
LEFT JOIN control_plane.attachment_sets attachment_set ON attachment_set.id=r.input_attachment_set_id
WHERE r.id=$1::uuid
