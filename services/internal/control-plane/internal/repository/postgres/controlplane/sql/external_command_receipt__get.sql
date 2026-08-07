SELECT issuer, purpose, receipt_id::text, organization_id::text,
       project_id::text, owner_actor_id::text, target_kind,
       coalesce(target_resource_id::text, ''), target_stable_key, action,
       effect, effect_generation, effect_sha256, command_intent_sha256,
       authority_sha256, coalesce(result_resource_id::text, ''),
       result_version, coalesce(result_sha256, ''), result_snapshot, consumed_at
FROM control_plane.external_command_receipt_consumptions
WHERE issuer = @issuer
  AND purpose = @purpose
  AND receipt_id = @receipt_id::uuid
FOR UPDATE
