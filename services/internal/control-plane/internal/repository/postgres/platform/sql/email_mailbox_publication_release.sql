-- name: email_mailbox_publication_release :exec
UPDATE control_plane.email_mailbox_publications SET claimant='',lease_expires_at=NULL
WHERE ref=$1 AND claimant=$2 AND claim_generation=$3 AND state IN ('PENDING','READY');
