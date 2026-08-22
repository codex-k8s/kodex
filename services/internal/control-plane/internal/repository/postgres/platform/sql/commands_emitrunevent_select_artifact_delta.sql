-- name: platform__commands_emitrunevent_select_artifact_delta :one
SELECT artifact.ref,
       project.ref,
       COALESCE(run.ref, ''),
       COALESCE(node.ref, ''),
       artifact.file_name,
       artifact.media_type,
       artifact.digest,
       artifact.scan_state,
       artifact.preview_state,
       artifact.size_bytes,
       artifact.version,
       artifact.created_at,
       COALESCE((
           SELECT array_agg(binding.target_kind || ':' || binding.target_ref ORDER BY binding.created_at)
           FROM control_plane.artifact_bindings binding
           WHERE binding.artifact_id = artifact.id
       ), '{}'::text[])
FROM control_plane.artifacts artifact
JOIN control_plane.projects project ON project.id = artifact.project_id
LEFT JOIN control_plane.runs run ON run.id = artifact.run_id
LEFT JOIN control_plane.run_nodes node ON node.id = artifact.node_id
WHERE artifact.organization_id = $1::uuid
  AND artifact.ref = $2
