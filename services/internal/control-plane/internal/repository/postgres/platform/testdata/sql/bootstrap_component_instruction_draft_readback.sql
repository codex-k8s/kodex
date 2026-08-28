-- name: bootstrap_component_instruction_draft_readback :one
SELECT count(*)::integer,
       min(state),
       min(content)
FROM control_plane.instruction_versions
WHERE agent_id = (SELECT id FROM control_plane.agents WHERE ref = $1)
  AND state IN ('DRAFT', 'VALID', 'INVALID')
