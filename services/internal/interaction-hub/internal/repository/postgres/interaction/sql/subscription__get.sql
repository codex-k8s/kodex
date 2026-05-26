-- name: subscription__get :one
SELECT
    id,
    scope_type,
    scope_ref,
    subscriber_ref_kind,
    subscriber_ref,
    event_filter,
    delivery_preferences,
    status,
    version,
    created_at,
    updated_at,
    source_owner_kind,
    source_owner_ref,
    channel_hint_refs,
    subscription_policy_ref
FROM interaction_hub_subscriptions
WHERE id = @id;
