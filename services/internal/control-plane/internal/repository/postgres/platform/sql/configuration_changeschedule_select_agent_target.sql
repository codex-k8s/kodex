-- name: configuration_changeschedule_select_agent_target :one
SELECT id::text,
       version,
       encode(digest(convert_to(ref || chr(31) || version::text, 'UTF8'), 'sha256'), 'hex')
FROM control_plane.agents
WHERE organization_id = $1::uuid
  AND project_id = $2::uuid
  AND ref = $3
  AND system_key IS NULL
  AND enabled
  AND state = 'READY'
