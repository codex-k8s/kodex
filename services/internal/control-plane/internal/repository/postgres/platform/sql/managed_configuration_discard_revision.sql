-- name: managed_configuration_discard_revision :one
UPDATE control_plane.managed_configuration_revisions AS discarded SET state='DISCARDED'
WHERE organization_id=@organization_id::uuid AND configuration_set_id=@configuration_set_id::uuid
  AND id=@revision_id::uuid AND state IN ('DRAFT','INVALID','VALID')
RETURNING id::text,ref,revision,state,content_format,content,digest,
    COALESCE((SELECT ref FROM control_plane.managed_configuration_revisions parent WHERE parent.id=discarded.parent_revision_id),''),
    validation_diagnostics,created_at,validated_at,published_at;
