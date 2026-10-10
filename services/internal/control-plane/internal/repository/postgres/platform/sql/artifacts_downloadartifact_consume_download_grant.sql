-- name: artifacts_downloadartifact_consume_download_grant :one
UPDATE control_plane.artifact_download_grants g
SET consumed_at = clock_timestamp()
WHERE g.id = @grant_id::uuid
  AND g.organization_id = @organization_id::uuid
  AND g.project_id IS NOT DISTINCT FROM NULLIF(@project_id, '')::uuid
  AND g.artifact_id = @artifact_id::uuid
  AND g.revision_id = @revision_id::uuid
  AND g.artifact_version = @artifact_version
  AND g.subject_id = @subject_id::uuid
  AND g.purpose = @purpose
  AND g.consumed_at IS NULL
  AND g.expires_at > clock_timestamp()
  AND EXISTS (
      SELECT 1
      FROM control_plane.artifact_history ar
      WHERE ar.id = g.artifact_id
        AND ar.organization_id = g.organization_id
        AND ar.project_id IS NOT DISTINCT FROM g.project_id
        AND ar.revision_id = g.revision_id
        AND ar.scan_state = 'CLEAN'
        AND ar.lifecycle_state = 'ACTIVE'
        AND EXISTS (
            SELECT 1 FROM control_plane.catalog_access_targets target
            WHERE target.organization_id=ar.organization_id AND target.kind='ARTIFACT' AND target.id=ar.id
              AND control_plane.catalog_resource_visible(ar.organization_id,g.subject_id,'artifact.view',target.kind,
                  target.id,target.project_id,target.owner_id,target.related_ids,transaction_timestamp())
              AND control_plane.catalog_resource_visible(ar.organization_id,g.subject_id,'artifact.download',target.kind,
                  target.id,target.project_id,target.owner_id,target.related_ids,transaction_timestamp())
        )
  )
RETURNING g.consumed_at;
