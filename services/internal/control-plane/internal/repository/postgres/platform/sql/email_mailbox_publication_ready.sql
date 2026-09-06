-- name: email_mailbox_publication_ready :exec
UPDATE control_plane.email_mailbox_publications
SET state='READY',ready_at=COALESCE(ready_at,clock_timestamp()),claimant='',lease_expires_at=NULL
WHERE ref=$1 AND claimant=$2 AND claim_generation=$3 AND lease_expires_at>clock_timestamp()
    AND state IN ('PENDING','READY') AND applied_at IS NOT NULL AND callback_at IS NOT NULL;
