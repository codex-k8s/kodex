-- name: email_report_terminal_source :exec
UPDATE control_plane.integration_invocations
SET state=$2,lease_ref=NULL,effect_fence_digest=NULL,lease_expires_at=NULL,workload_instance=NULL,version=version+1
WHERE ref=$1 AND state IN ('UNKNOWN_OUTCOME','CANCELLED','FAILED');
