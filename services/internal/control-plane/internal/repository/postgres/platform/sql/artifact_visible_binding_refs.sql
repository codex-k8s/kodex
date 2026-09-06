-- name: artifact_visible_binding_refs :many
SELECT agent.ref
FROM control_plane.artifacts artifact
JOIN control_plane.artifact_bindings binding ON binding.artifact_id=artifact.id AND binding.target_kind='KNOWLEDGE'
JOIN control_plane.agents agent ON agent.ref=binding.target_ref
  AND agent.organization_id=artifact.organization_id AND agent.project_id=artifact.project_id
WHERE artifact.organization_id=@organization_id::uuid AND artifact.ref=@artifact_ref
  AND agent.ref=ANY(@agent_refs::text[]) AND agent.system_key IS NULL
  AND (@authority_project_id='' OR artifact.project_id=NULLIF(@authority_project_id,'')::uuid)
  AND control_plane.catalog_resource_visible(@organization_id::uuid,@actor_id::uuid,'agent.view',
    'AGENT',agent.id,agent.project_id,agent.created_by,
    jsonb_build_object('PROJECT',agent.project_id::text),transaction_timestamp())
