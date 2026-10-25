-- name: repository_bundle__artifact_eligibility :one
SELECT history.ref
FROM control_plane.artifact_heads artifact
JOIN control_plane.artifact_history history
  ON history.id=artifact.id AND history.organization_id=artifact.organization_id
JOIN control_plane.catalog_access_targets target
  ON target.organization_id=artifact.organization_id AND target.kind='ARTIFACT' AND target.id=artifact.id
WHERE artifact.organization_id=@organization_id::uuid AND artifact.project_id=@project_id::uuid
  AND artifact.ref=@artifact_ref AND artifact.lifecycle_state='ACTIVE'
  AND history.revision=@revision AND history.digest='sha256:'||@digest AND history.size_bytes=@size_bytes
  AND history.scan_state='CLEAN'
  AND control_plane.catalog_resource_visible(artifact.organization_id,@actor_id::uuid,'artifact.view','ARTIFACT',target.id,target.project_id,target.owner_id,target.related_ids,statement_timestamp(),false)
  AND control_plane.catalog_resource_visible(artifact.organization_id,@actor_id::uuid,'artifact.download','ARTIFACT',target.id,target.project_id,target.owner_id,target.related_ids,statement_timestamp(),false)
FOR SHARE OF artifact NOWAIT;
