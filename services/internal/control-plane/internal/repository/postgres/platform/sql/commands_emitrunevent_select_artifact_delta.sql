-- name: commands_emitrunevent_select_artifact_delta :one
SELECT artifact.ref,
       COALESCE(project.ref, ''),
       COALESCE(run.ref, ''),
       COALESCE(session.ref, ''),
       COALESCE(node.ref, ''),
       artifact.file_name,
       artifact.media_type,
       artifact.digest,
       artifact.scan_state,
       artifact.preview_state,
       artifact.source,
       artifact.size_bytes,
       artifact.revision,
       artifact.version,
       artifact.lifecycle_state,
       artifact.created_at,
       artifact.deleted_at,
       artifact.purge_after,
       COALESCE((
           SELECT array_agg(binding.target_ref ORDER BY binding.created_at)
           FROM control_plane.artifact_bindings binding
           WHERE binding.artifact_id = artifact.id
             AND binding.target_kind = 'KNOWLEDGE'
       ), '{}'::text[]),
       false
FROM control_plane.artifacts artifact
LEFT JOIN control_plane.projects project ON project.id = artifact.project_id
LEFT JOIN control_plane.runs run ON run.id = artifact.run_id
LEFT JOIN control_plane.sessions session ON session.id = run.session_id
LEFT JOIN control_plane.run_nodes node ON node.id = artifact.node_id
WHERE artifact.organization_id = $1::uuid
  AND artifact.ref = $2
