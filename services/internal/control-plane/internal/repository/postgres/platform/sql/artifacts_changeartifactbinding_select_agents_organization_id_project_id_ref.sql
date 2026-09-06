-- name: artifacts_changeartifactbinding_select_agents_organization_id_project_id_ref :one
SELECT id::text,
       @capability::text=ANY(capabilities)
FROM control_plane.agents a
WHERE organization_id=@organization_id::uuid
  AND project_id=@project_id::uuid
  AND ref=@agent_ref
  AND system_key IS NULL
  AND (state<>'ARCHIVED' OR (NOT @enabled::boolean AND EXISTS (
    SELECT 1 FROM control_plane.artifact_bindings b
    WHERE b.artifact_id=@artifact_id::uuid AND b.target_kind='KNOWLEDGE'
      AND b.target_ref=a.ref)))
FOR UPDATE
