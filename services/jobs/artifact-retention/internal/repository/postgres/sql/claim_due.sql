-- name: claim_due :many
WITH candidate AS (
    SELECT artifact.id,
           content.object_key,
           content.object_version
    FROM control_plane.artifacts AS artifact
    JOIN control_plane.artifact_content AS content ON content.artifact_id = artifact.id
    WHERE (
        (
            artifact.lifecycle_state = 'DELETED'
            AND artifact.purge_after <= clock_timestamp()
        ) OR (
            artifact.lifecycle_state = 'PURGE_PENDING'
            AND (
                artifact.retention_claim_expires_at IS NULL
                OR artifact.retention_claim_expires_at <= clock_timestamp()
            )
        )
    )
      AND NOT EXISTS (
          SELECT 1
          FROM control_plane.runs AS active_run
          WHERE active_run.state IN ('QUEUED', 'RUNNING', 'WAITING_HUMAN', 'CANCELLING')
            AND (
                EXISTS (
                    SELECT 1
                    FROM control_plane.attachment_set_items AS input_item
                    WHERE input_item.attachment_set_id = active_run.input_attachment_set_id
                      AND input_item.artifact_id = artifact.id
                      AND input_item.artifact_revision = artifact.revision
                )
                OR EXISTS (
                    SELECT 1
                    FROM control_plane.session_turns AS current_turn
                    JOIN control_plane.attachment_set_items AS turn_item
                      ON turn_item.attachment_set_id = current_turn.attachment_set_id
                    WHERE current_turn.run_id = active_run.id
                      AND turn_item.artifact_id = artifact.id
                      AND turn_item.artifact_revision = artifact.revision
                )
                OR EXISTS (
                    SELECT 1
                    FROM control_plane.session_turns AS source_turn
                    JOIN control_plane.attachment_set_items AS history_item
                      ON history_item.attachment_set_id = source_turn.attachment_set_id
                    WHERE source_turn.session_id = active_run.session_id
                      AND source_turn.created_at < active_run.created_at
                      AND history_item.artifact_id = artifact.id
                      AND history_item.artifact_revision = artifact.revision
                      AND artifact.deleted_at > active_run.created_at
                )
                OR EXISTS (
                    SELECT 1
                    FROM control_plane.runtime_revisions AS runtime_revision
                    WHERE runtime_revision.root_run_id = active_run.id
                      AND EXISTS (
                          SELECT 1
                          FROM jsonb_array_elements(COALESCE(runtime_revision.safe_snapshot -> 'artifacts', '[]'::jsonb)) AS exact(item)
                          WHERE exact.item ->> 'ref' = artifact.ref
                            AND exact.item -> 'revision' = to_jsonb(artifact.revision)
                            AND exact.item ->> 'digest' = artifact.digest
                      )
                )
            )
      )
    ORDER BY artifact.purge_after, artifact.id
    FOR UPDATE OF artifact SKIP LOCKED
    LIMIT @batch_size
), claimed AS (
    UPDATE control_plane.artifacts AS artifact
    SET lifecycle_state = 'PURGE_PENDING',
        retention_claim_owner = @claim_owner,
        retention_claim_generation = artifact.retention_claim_generation + 1,
        retention_claim_expires_at = clock_timestamp() + (@lease_seconds * interval '1 second'),
        version = artifact.version + 1
    FROM candidate
    WHERE artifact.id = candidate.id
    RETURNING artifact.id,
              artifact.ref,
              artifact.retention_claim_generation
)
SELECT claimed.id::text,
       claimed.ref,
       candidate.object_key,
       candidate.object_version,
       claimed.retention_claim_generation
FROM claimed
JOIN candidate ON candidate.id = claimed.id
ORDER BY claimed.id;
