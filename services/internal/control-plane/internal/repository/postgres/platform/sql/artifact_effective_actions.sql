-- name: artifact_effective_actions :one
SELECT artifact.version, artifact.lifecycle_state, artifact.scan_state, ARRAY(
    SELECT permission FROM unnest(ARRAY['artifact.download','artifact.bind','artifact.delete','artifact.restore','artifact.purge']) permission
    WHERE control_plane.catalog_resource_visible(@organization_id::uuid,@actor_id::uuid,permission,
        target.kind,target.id,target.project_id,target.owner_id,target.related_ids,transaction_timestamp())
)
FROM control_plane.catalog_access_targets target
JOIN control_plane.artifacts artifact ON artifact.id=target.id AND artifact.organization_id=target.organization_id
WHERE target.organization_id=@organization_id::uuid AND target.kind='ARTIFACT' AND target.ref=@artifact_ref
  AND (@authority_project='' OR target.project_id=NULLIF(@authority_project,'')::uuid)
  AND control_plane.catalog_resource_visible(@organization_id::uuid,@actor_id::uuid,'artifact.view',
      target.kind,target.id,target.project_id,target.owner_id,target.related_ids,transaction_timestamp());
