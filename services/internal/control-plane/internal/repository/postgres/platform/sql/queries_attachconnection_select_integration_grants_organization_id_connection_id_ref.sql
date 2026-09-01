-- name: queries_attachconnection_select_integration_grants_organization_id_connection_id_ref :many
SELECT g.ref,g.capability_key,g.target_kind,g.target_ref,COALESCE(a.name,w.name,g.target_ref),g.enabled,g.approval_policy,g.version,
	g.risk,g.resource_kind,g.resource_scope,g.resource_scope_digest
FROM control_plane.integration_grants g
LEFT JOIN control_plane.agents a ON g.target_kind='AGENT' AND a.ref=g.target_ref AND a.organization_id=g.organization_id
LEFT JOIN control_plane.workflows w ON g.target_kind='WORKFLOW' AND w.ref=g.target_ref AND w.organization_id=g.organization_id
WHERE g.organization_id=$1::uuid
  AND g.connection_id=(SELECT id FROM control_plane.integration_connections WHERE organization_id=$1::uuid AND ref=$2)
  AND ($3 IN ('OWNER','ADMINISTRATOR') OR EXISTS(
      SELECT 1 FROM control_plane.memberships m
      WHERE m.organization_id=g.organization_id AND m.subject_id=$4::uuid AND m.active
        AND m.project_id=COALESCE(a.project_id,w.project_id)
        AND ('VIEW'=ANY(m.permissions) OR 'MANAGE_INTEGRATIONS'=ANY(m.permissions))))
ORDER BY g.created_at
