-- name: subscription__update :exec
UPDATE interaction_hub_subscriptions
SET scope_type = @scope_type,
    scope_ref = @scope_ref,
    subscriber_ref_kind = @subscriber_ref_kind,
    subscriber_ref = @subscriber_ref,
    event_filter = @event_filter,
    delivery_preferences = @delivery_preferences,
    status = @status,
    version = @version,
    updated_at = @updated_at,
    source_owner_kind = @source_owner_kind,
    source_owner_ref = @source_owner_ref,
    channel_hint_refs = @channel_hint_refs,
    subscription_policy_ref = @subscription_policy_ref
WHERE id = @id
  AND version = @previous_version;
