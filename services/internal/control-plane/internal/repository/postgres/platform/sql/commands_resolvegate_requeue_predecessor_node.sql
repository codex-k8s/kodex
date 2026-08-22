-- name: platform__commands_resolvegate_requeue_predecessor_node :exec
UPDATE control_plane.run_nodes
SET state='QUEUED',
    turn_id=$2::uuid,
    attempt=attempt+1,
    input_summary=$3,
    progress_summary='',
    callback_summary='',
    safe_error_code='',
    safe_error_message='',
    next_actions='{}'::text[],
    started_at=NULL,
    finished_at=NULL,
    version=version+1
WHERE id=$1::uuid
  AND state='SUCCEEDED'
