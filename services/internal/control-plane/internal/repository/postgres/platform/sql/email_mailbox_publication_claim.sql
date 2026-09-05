-- name: email_mailbox_publication_claim :one
WITH candidate AS (
    SELECT ref FROM control_plane.email_mailbox_publications
    WHERE state IN ('PENDING','READY') AND revision=(SELECT max(revision) FROM control_plane.email_mailbox_publications)
        AND (lease_expires_at IS NULL OR lease_expires_at<=clock_timestamp())
    FOR UPDATE SKIP LOCKED
)
UPDATE control_plane.email_mailbox_publications publication
SET claimant=$1,claim_generation=claim_generation+1,lease_expires_at=clock_timestamp()+interval '45 seconds'
FROM candidate WHERE publication.ref=candidate.ref
RETURNING publication.ref,publication.state,publication.claim_generation,publication.document,publication.digest,
    publication.policy_document,publication.applied_at IS NOT NULL,publication.callback_at IS NOT NULL,publication.expires_at;
