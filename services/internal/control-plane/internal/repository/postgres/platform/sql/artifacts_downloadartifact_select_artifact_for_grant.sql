-- name: artifacts_downloadartifact_select_artifact_for_grant :one
SELECT ar.id,
       COALESCE(ar.project_id::text, ''),
       ar.version,
       ar.scan_state,ar.revision_id,ar.revision_ref
FROM control_plane.artifact_history ar
JOIN control_plane.artifact_heads head ON head.id=ar.id
WHERE ar.organization_id = @organization_id::uuid
  AND ar.ref = @artifact_ref
  AND ar.lifecycle_state = 'ACTIVE'
  AND ((@revision_ref='' AND ar.revision_id=head.current_revision_id) OR ar.revision_ref=@revision_ref)
  AND (@authority_project='' OR ar.project_id=NULLIF(@authority_project,'')::uuid)
  AND EXISTS (
      SELECT 1 FROM control_plane.catalog_access_targets target
      WHERE target.organization_id=ar.organization_id AND target.kind='ARTIFACT' AND target.id=ar.id
        AND control_plane.catalog_resource_visible(ar.organization_id,@subject_id::uuid,'artifact.view',target.kind,
            target.id,target.project_id,target.owner_id,target.related_ids,transaction_timestamp())
        AND control_plane.catalog_resource_visible(ar.organization_id,@subject_id::uuid,'artifact.download',target.kind,
            target.id,target.project_id,target.owner_id,target.related_ids,transaction_timestamp())
  )
FOR UPDATE OF head;
