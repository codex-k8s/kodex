-- name: workflow_evidence_deadline_wait :one
SELECT GREATEST(0,ceil(extract(epoch FROM(execution_deadline_at-clock_timestamp()))*1000))::bigint
FROM control_plane.runs WHERE ref=$1 AND id=root_run_id AND execution_deadline_at IS NOT NULL;
