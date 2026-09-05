-- name: context_binding_get :one
SELECT binding.ref,binding.version,binding.enabled,COALESCE(memory.ref,skill.ref)
FROM control_plane.agent_context_bindings binding
LEFT JOIN control_plane.memory_record_revisions memory ON memory.id=binding.memory_revision_id
LEFT JOIN control_plane.skill_bundle_revisions skill ON skill.id=binding.skill_revision_id
WHERE binding.organization_id=$1::uuid AND binding.agent_id=$2::uuid
    AND (binding.memory_record_id=NULLIF($3,'')::uuid OR binding.skill_bundle_id=NULLIF($4,'')::uuid)
FOR UPDATE OF binding;
