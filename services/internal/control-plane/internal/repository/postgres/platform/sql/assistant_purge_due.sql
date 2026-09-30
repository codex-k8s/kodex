-- name: assistant_purge_due :many
WITH candidates AS (
    SELECT organization_id,created_by,ref,version
    FROM control_plane.assistant_conversations
    WHERE state='ARCHIVED' AND purge_after <= clock_timestamp()
    ORDER BY purge_after,id
    LIMIT @batch_size
    FOR UPDATE SKIP LOCKED
)
SELECT candidate.ref, control_plane.purge_assistant_conversation(
    candidate.organization_id, candidate.created_by, candidate.ref,
    candidate.version, candidate.created_by, 'RETENTION'
)
FROM candidates candidate;
