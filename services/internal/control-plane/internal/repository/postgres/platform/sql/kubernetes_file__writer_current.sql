-- name: kubernetes_file__writer_current :one
SELECT source.id::text FROM control_plane.kubernetes_file_input_sources source
JOIN control_plane.kubernetes_file_input_origins origin ON origin.invocation_id=source.invocation_id AND origin.organization_id=source.organization_id
JOIN control_plane.integration_invocations invocation ON invocation.id=source.invocation_id AND invocation.organization_id=source.organization_id
JOIN control_plane.runtime_file_catalog_entries entry ON entry.id=origin.entry_id AND encode(entry.entry_digest,'hex')=origin.entry_digest
JOIN control_plane.runtime_file_catalogs catalog ON catalog.id=entry.catalog_id AND catalog.id=origin.catalog_id
WHERE source.ref=@source_ref AND source.organization_id=@organization_id::uuid AND source.prepared_content_id=@prepared_id::uuid
 AND source.state='PREPARING' AND source.payload_purged_at IS NULL AND source.writer_completed_at IS NULL
 AND source.writer_deadline>clock_timestamp() AND source.authorized_until>clock_timestamp() AND source.hard_deadline>clock_timestamp()
 AND invocation.state='RUNNING' AND invocation.lease_expires_at>clock_timestamp()
 AND invocation.generation=source.invocation_generation AND invocation.effect_fence_digest=source.original_fence_digest
 AND control_plane.kubernetes_execution_child_grant_current(source.child_id,invocation.id)
 AND control_plane.runtime_file_source_visible_revision(catalog.organization_id,catalog.actor_id,catalog.project_id,catalog.agent_id,
  origin.artifact_id,entry.purpose,entry.source_revision_ref,catalog.node_id,entry.id);
