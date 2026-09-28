-- name: commands_resolvegate_insert_integration_approval_scope :one
INSERT INTO control_plane.integration_approval_scopes(
    organization_id,project_id,connection_id,grant_id,root_run_id,agent_id,origin_gate_id,
    capability_key,grant_version,definition_digest,input_schema_digest,
    scope_paths,scope_values,scope_digest,max_effects,reserved_effects,expires_at
)
VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::uuid,$7::uuid,
       $8,$9,$10,$11,$12::text[],$13::jsonb,$14,100,1,clock_timestamp()+interval '24 hours')
RETURNING id::text
