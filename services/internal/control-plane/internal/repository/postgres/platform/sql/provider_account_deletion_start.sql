-- name: provider_account_deletion_start :exec
INSERT INTO control_plane.provider_account_deletion_intents (
    ref, organization_id, provider_account_id, requested_by, state, safe_reason
) VALUES (
    @intent_ref, @organization_id::uuid, @account_id::uuid, @actor_id::uuid,
    'PENDING_BLOCKERS', 'WAITING_FOR_DEPENDENCIES'
);
