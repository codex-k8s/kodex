-- name: session_archive_select_claimable_tasks :many
SELECT task.id::text, task.ref, task.kind, task.content_generation, task.input_digest,
       COALESCE(task.object_key, ''), COALESCE(task.object_version, ''), task.attempt,
       organization.ref, COALESCE(project.ref, ''), session.ref, provider.ref,
       revision.ref, revision.generation, revision.revision_digest,
       storage.codex_session_id::text, storage.source_relative_path,
       storage.source_sha256, storage.source_size_bytes,
       COALESCE(archive.ref, ''), COALESCE(archive.format_version, 0),
       COALESCE(archive.object_key, ''), COALESCE(archive.object_version, ''),
       COALESCE(archive.object_etag, ''), COALESCE(archive.object_digest, ''),
       COALESCE(archive.object_size_bytes, 0), COALESCE(archive.source_relative_path, ''),
       COALESCE(archive.source_sha256, ''), COALESCE(archive.source_size_bytes, 0)
FROM control_plane.session_archive_tasks task
JOIN control_plane.organizations organization ON organization.id = task.organization_id
LEFT JOIN control_plane.projects project ON project.id = task.project_id
JOIN control_plane.sessions session ON session.id = task.session_id
JOIN control_plane.provider_accounts provider ON provider.id = session.provider_account_id
JOIN control_plane.session_storage storage ON storage.session_id = task.session_id
JOIN control_plane.runtime_revisions revision ON revision.id = storage.runtime_revision_id
LEFT JOIN control_plane.session_archives archive ON archive.id = task.archive_id
WHERE task.organization_id = @organization_id::uuid
  AND task.state = 'READY'
  AND task.available_at <= clock_timestamp()
ORDER BY CASE task.kind WHEN 'RESTORE' THEN 0 WHEN 'DELETE_PVC' THEN 1 WHEN 'SNAPSHOT' THEN 2 ELSE 3 END,
         task.created_at
FOR UPDATE OF task SKIP LOCKED
LIMIT @limit;
