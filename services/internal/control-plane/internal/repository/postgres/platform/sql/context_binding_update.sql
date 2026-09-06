-- name: context_binding_update :one
UPDATE control_plane.agent_context_bindings SET version=version+1,enabled=@enabled,
    memory_revision_id=NULLIF(@memory_revision_id,'')::uuid,skill_revision_id=NULLIF(@skill_revision_id,'')::uuid,updated_at=clock_timestamp()
WHERE organization_id=@organization_id::uuid AND ref=@ref AND version=@version
RETURNING version;
