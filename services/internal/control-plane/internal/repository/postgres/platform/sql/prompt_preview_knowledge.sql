-- name: prompt_preview_knowledge :many
SELECT artifact.ref,artifact.file_name,artifact.media_type,artifact.digest,
       artifact.size_bytes,artifact.revision,artifact.version,artifact.source,
       row_number() OVER (ORDER BY binding.created_at,artifact.ref)
FROM control_plane.artifact_bindings binding
JOIN control_plane.agents agent ON agent.ref=binding.target_ref
JOIN control_plane.artifact_history artifact ON artifact.id=binding.artifact_id AND artifact.revision_id=binding.revision_id
JOIN control_plane.artifact_revision_content content ON content.revision_id=artifact.revision_id
WHERE binding.target_kind='KNOWLEDGE' AND binding.target_ref=@agent_ref
  AND agent.organization_id=@organization_id::uuid
  AND artifact.organization_id=agent.organization_id AND artifact.project_id=agent.project_id
  AND artifact.scan_state='CLEAN' AND artifact.lifecycle_state='ACTIVE'
  AND content.digest=artifact.digest AND content.size_bytes=artifact.size_bytes
ORDER BY binding.created_at,artifact.ref LIMIT 2049;
