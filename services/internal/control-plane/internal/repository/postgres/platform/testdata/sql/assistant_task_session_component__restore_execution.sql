-- name: assistant_task_session_component__restore_execution :exec
UPDATE control_plane.run_events SET safe_delta=jsonb_set(safe_delta,'{Execution}',$2::jsonb) WHERE ref=$1;
