-- name: assistant_task_session_component__source_version :exec
UPDATE control_plane.runs SET version=version+1 WHERE ref=$1;
