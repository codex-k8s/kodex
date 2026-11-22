-- name: claim_due :many
WITH candidate AS (
    SELECT artifact.id, content.revision_id, content.object_key, content.object_version
    FROM control_plane.artifact_heads artifact
    CROSS JOIN LATERAL (
        SELECT content.revision_id, content.object_key, content.object_version
        FROM control_plane.artifact_revisions revision
        JOIN control_plane.artifact_revision_content content ON content.revision_id=revision.id
        WHERE revision.artifact_id=artifact.id
        ORDER BY revision.revision, revision.id LIMIT 1
    ) content
    WHERE ((artifact.lifecycle_state='DELETED' AND artifact.purge_after<=clock_timestamp())
        OR (artifact.lifecycle_state='PURGE_PENDING' AND
            (artifact.retention_claim_expires_at IS NULL OR artifact.retention_claim_expires_at<=clock_timestamp())))
      AND artifact.manual_purge_actor_id IS NULL
      AND NOT control_plane.artifact_has_retained_revisions(artifact.id)
    ORDER BY artifact.purge_after, artifact.id
    FOR UPDATE OF artifact SKIP LOCKED
    LIMIT @batch_size::integer
), claimed AS (
    UPDATE control_plane.artifact_heads artifact
    SET lifecycle_state='PURGE_PENDING', retention_claim_owner=@claim_owner,
        retention_claim_generation=artifact.retention_claim_generation + 1,
        retention_claim_expires_at=clock_timestamp() + (@lease_seconds::integer * interval '1 second'),
        version=artifact.version+1
    FROM candidate WHERE artifact.id=candidate.id
    RETURNING artifact.id,artifact.ref,artifact.retention_claim_generation,artifact.retention_claim_expires_at
)
SELECT claimed.id::text,claimed.ref,candidate.revision_id::text,
       candidate.object_key,candidate.object_version,claimed.retention_claim_generation,
       false,false,claimed.retention_claim_expires_at
FROM claimed JOIN candidate ON candidate.id=claimed.id
UNION ALL
SELECT manual.artifact_id::text,manual.artifact_ref,manual.revision_id::text,
 manual.object_key,manual.object_version,manual.generation,manual.manual,manual.reconcile_only,manual.authorized_until
FROM control_plane.artifact_manual_retention_claim(@claim_owner,
 greatest(@batch_size::integer-(SELECT count(*)::integer FROM claimed),0),@lease_seconds::integer) manual;
