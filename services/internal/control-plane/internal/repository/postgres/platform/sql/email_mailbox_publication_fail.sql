-- name: email_mailbox_publication_fail :exec
UPDATE control_plane.email_mailbox_publications
SET state=CASE WHEN state='READY' THEN 'SUPERSEDED' ELSE 'FAILED' END,
    failure_code=CASE WHEN state='READY' THEN '' ELSE $4 END,claimant='',lease_expires_at=NULL
WHERE ref=$1 AND claimant=$2 AND claim_generation=$3 AND lease_expires_at>clock_timestamp()
    AND state IN ('PENDING','READY');
