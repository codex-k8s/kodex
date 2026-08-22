-- name: platform__runtime_delegateexecution_3 :one
SELECT session_id::text,initiated_by::text,id::text FROM control_plane.runs WHERE id=$1::uuid
