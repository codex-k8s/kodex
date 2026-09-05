-- name: email_mailbox_publication_applied :exec
UPDATE control_plane.email_mailbox_publications SET applied_at=COALESCE(applied_at,clock_timestamp())
WHERE ref=$1 AND claimant=$2 AND claim_generation=$3 AND lease_expires_at>clock_timestamp()
    AND state IN ('PENDING','READY') AND policy_document IS NOT NULL;
