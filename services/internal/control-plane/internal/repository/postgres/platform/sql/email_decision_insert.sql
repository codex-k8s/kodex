-- name: EmailDecisionInsert :exec
INSERT INTO control_plane.email_reconciliation_decisions
    (ref,organization_id,receipt_id,receipt_version,receipt_digest,outcome,grant_ref,actor_id,note,created_at,expires_at)
VALUES (@ref,@organization_id::uuid,@receipt_id::uuid,@receipt_version,@receipt_digest,@outcome,
        @grant_ref,@actor_id::uuid,@note,@created_at,@expires_at);
