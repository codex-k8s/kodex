INSERT INTO control_plane.external_command_receipt_consumptions (
    issuer, purpose, receipt_id, organization_id, project_id, owner_actor_id,
    target_kind, target_resource_id, target_stable_key, action, effect,
    effect_generation, effect_sha256, command_intent_sha256,
    authority_sha256, consumed_at
) VALUES (
    @issuer, @purpose, @receipt_id::uuid, @organization_id::uuid,
    @project_id::uuid, @owner_actor_id::uuid, @target_kind,
    nullif(@target_resource_id, '')::uuid, @target_stable_key, @action,
    @effect, @effect_generation, @effect_sha256, @command_intent_sha256,
    @authority_sha256, @consumed_at
)
ON CONFLICT (issuer, purpose, receipt_id) DO NOTHING
