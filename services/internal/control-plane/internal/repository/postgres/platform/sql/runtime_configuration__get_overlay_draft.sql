-- name: runtime_configuration__get_overlay_draft :one
SELECT overlay_version.id::text,
       overlay_version.ref,
       overlay_version.version_number,
       overlay_version.state,
       overlay_version.content
FROM control_plane.agents agent
JOIN control_plane.agent_config_overlay_versions overlay_version ON overlay_version.agent_id = agent.id
WHERE agent.organization_id = $1::uuid
  AND agent.ref = $2
  AND overlay_version.state IN ('DRAFT', 'VALID', 'INVALID')
ORDER BY overlay_version.version_number DESC
LIMIT 1
FOR UPDATE OF overlay_version;
