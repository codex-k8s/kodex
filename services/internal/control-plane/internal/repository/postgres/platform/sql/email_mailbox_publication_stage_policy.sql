-- name: email_mailbox_publication_stage_policy :exec
UPDATE control_plane.email_mailbox_publications
SET policy_document=$4::jsonb,policy_digest=$5
WHERE ref=$1 AND claimant=$2 AND claim_generation=$3 AND lease_expires_at>clock_timestamp()
    AND digest=$6 AND revision=$7
    AND state IN ('PENDING','READY') AND (policy_document IS NULL OR (policy_document=$4::jsonb AND policy_digest=$5));
