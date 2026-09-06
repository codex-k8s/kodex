-- name: context_binding_insert :one
INSERT INTO control_plane.agent_context_bindings(ref,organization_id,project_id,agent_id,memory_record_id,memory_revision_id,skill_bundle_id,skill_revision_id,created_by)
VALUES (@ref,@organization_id::uuid,@project_id::uuid,@agent_id::uuid,NULLIF(@memory_id,'')::uuid,NULLIF(@memory_revision_id,'')::uuid,
    NULLIF(@skill_id,'')::uuid,NULLIF(@skill_revision_id,'')::uuid,@actor_id::uuid)
RETURNING version;
