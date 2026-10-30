-- name: workspace_restore__source :one
SELECT archive.id::text,o.ref,p.ref,s.ref,a.ref,w.ref,archive.workspace_generation,
 archive.workspace_checkpoint_ref,archive.source_sha256,archive.source_size_bytes,archive.workspace_entries,
 archive.object_key,archive.object_version,archive.object_etag,archive.object_digest,archive.object_size_bytes
FROM control_plane.session_workspace_storage w
JOIN control_plane.session_archives archive ON archive.id=CASE WHEN w.state='CHECKPOINT_READY' THEN w.current_archive_id ELSE w.source_archive_id END
 AND archive.organization_id=w.organization_id AND archive.project_id=w.project_id AND archive.session_id=w.session_id AND archive.workspace_id=w.id
JOIN control_plane.runtime_revisions revision ON revision.id=archive.runtime_revision_id AND revision.organization_id=w.organization_id
 AND revision.project_id=w.project_id AND revision.session_id=w.session_id AND revision.agent_id=w.agent_id
JOIN control_plane.organizations o ON o.id=w.organization_id
JOIN control_plane.projects p ON p.id=w.project_id AND p.organization_id=w.organization_id
JOIN control_plane.sessions s ON s.id=w.session_id AND s.organization_id=w.organization_id AND s.project_id=w.project_id
JOIN control_plane.agents a ON a.id=w.agent_id AND a.organization_id=w.organization_id AND a.project_id=w.project_id
WHERE w.organization_id=@organization_id::uuid AND w.ref=@workspace_ref AND w.generation=@workspace_generation
 AND archive.format_version=2 AND archive.workspace_checkpoint_ref IS NOT NULL
 AND (w.state='RESTORED' OR archive.lifecycle_state='AVAILABLE' AND archive.deleted_at IS NULL)
 AND (w.state='CHECKPOINT_READY' AND archive.workspace_generation=w.generation
   OR w.state IN('RESTORE_READY','RESTORING','RESTORED') AND archive.workspace_generation=w.generation-1);
