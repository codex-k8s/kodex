-- name: instruction_binding_assign :one
INSERT INTO control_plane.agent_instruction_bindings(ref,organization_id,agent_id,instruction_id)
SELECT @binding_ref,@organization_id::uuid,a.id,i.id
FROM control_plane.agents a
JOIN control_plane.instruction_versions i ON i.agent_id=a.id AND i.organization_id=a.organization_id
WHERE a.id=@agent_id::uuid AND a.organization_id=@organization_id::uuid
 AND i.ref=@instruction_ref AND i.state='PUBLISHED' AND i.published_at IS NOT NULL
ON CONFLICT(agent_id) DO UPDATE
 SET instruction_id=EXCLUDED.instruction_id,version=agent_instruction_bindings.version+1,updated_at=clock_timestamp()
RETURNING ref,version;
