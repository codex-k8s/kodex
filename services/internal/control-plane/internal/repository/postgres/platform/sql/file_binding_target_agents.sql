-- name: file_binding_target_agents :many
SELECT a.id::text, a.ref, a.version, a.name, a.state,
       owner_subject.ref, @capability::text = ANY(a.capabilities),
       EXISTS (SELECT 1 FROM control_plane.artifact_bindings b
               WHERE b.artifact_id = @artifact_id::uuid
                 AND b.target_kind = 'KNOWLEDGE' AND b.target_ref = a.ref)
FROM control_plane.agents a
JOIN control_plane.subjects owner_subject ON owner_subject.id = a.created_by
WHERE a.organization_id = @organization_id::uuid AND a.project_id = @project_id::uuid
  AND a.system_key IS NULL
  AND (@agent_ref = '' OR a.ref = @agent_ref)
  AND a.ref > @after_ref
  AND (@query = '' OR position(lower(@query) IN lower(a.name)) > 0)
  AND (a.state <> 'ARCHIVED' OR @agent_ref <> '' OR EXISTS (
    SELECT 1 FROM control_plane.artifact_bindings b
    WHERE b.artifact_id = @artifact_id::uuid AND b.target_kind = 'KNOWLEDGE'
      AND b.target_ref = a.ref))
ORDER BY a.ref
LIMIT 100
