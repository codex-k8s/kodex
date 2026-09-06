-- name: integration_candidate_pins :one
SELECT COALESCE((SELECT version FROM control_plane.integration_connections WHERE organization_id=@organization_id::uuid AND ref=@connection_ref),0),
 COALESCE((SELECT definition_version FROM control_plane.integration_connections WHERE organization_id=@organization_id::uuid AND ref=@connection_ref),''),
 COALESCE((SELECT definition_digest FROM control_plane.integration_connections WHERE organization_id=@organization_id::uuid AND ref=@connection_ref),''),
 COALESCE((SELECT version FROM control_plane.projects WHERE organization_id=@organization_id::uuid AND ref=@project_ref),0),
 CASE @recipient_kind
  WHEN 'AGENT' THEN COALESCE((SELECT version FROM control_plane.agents WHERE organization_id=@organization_id::uuid AND ref=@recipient_ref),0)
  WHEN 'WORKFLOW' THEN COALESCE((SELECT version FROM control_plane.workflows WHERE organization_id=@organization_id::uuid AND ref=@recipient_ref),0)
  ELSE 0 END
