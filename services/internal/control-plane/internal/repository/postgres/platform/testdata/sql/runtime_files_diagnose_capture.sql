SELECT catalog.total,
  (SELECT count(*) FROM control_plane.runtime_file_catalog_entries WHERE catalog_id=catalog.id),
  ARRAY(SELECT DISTINCT permission||'='||control_plane.catalog_resource_visible(catalog.organization_id,catalog.actor_id,permission,
      target.kind,target.id,target.project_id,target.owner_id,target.related_ids,statement_timestamp())::text
    FROM control_plane.catalog_access_targets target CROSS JOIN unnest(ARRAY['artifact.view','artifact.download']) permission
    WHERE target.organization_id=catalog.organization_id AND target.kind='ARTIFACT' AND target.project_id=catalog.project_id),
  ARRAY(SELECT DISTINCT file.lifecycle_state||'/'||file.scan_state||'/'||control_plane.runtime_file_source_visible(
      catalog.organization_id,catalog.actor_id,catalog.project_id,catalog.agent_id,file.id,'WORKSPACE_INPUT','')::text
    FROM control_plane.artifacts file WHERE file.organization_id=catalog.organization_id AND file.project_id=catalog.project_id)
FROM control_plane.runtime_file_catalogs catalog WHERE catalog.ref=$1;
