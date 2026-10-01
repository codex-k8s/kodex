-- name: assistant_purge_due :many
WITH candidates AS (
    SELECT conversation.organization_id,organization.ref AS organization_ref,
           conversation.created_by,conversation.ref,conversation.version,
           COALESCE(project.ref,'') AS project_ref
    FROM control_plane.assistant_conversations conversation
    JOIN control_plane.organizations organization
      ON organization.id=conversation.organization_id
    LEFT JOIN control_plane.projects project ON project.id=conversation.project_id
    WHERE conversation.state='ARCHIVED' AND conversation.purge_after <= clock_timestamp()
    ORDER BY conversation.purge_after,conversation.id
    LIMIT @batch_size
    FOR UPDATE OF conversation SKIP LOCKED
)
SELECT candidate.organization_id::text,candidate.organization_ref,
       candidate.project_ref,candidate.ref,candidate.version,
       control_plane.purge_assistant_conversation(
    candidate.organization_id, candidate.created_by, candidate.ref,
    candidate.version, candidate.created_by, 'RETENTION'
)
FROM candidates candidate;
