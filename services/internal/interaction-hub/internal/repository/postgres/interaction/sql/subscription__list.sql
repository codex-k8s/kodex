-- name: subscription__list :many
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
WHERE scope_type = @scope_type
  AND scope_ref = @scope_ref
  AND (@subscriber_ref::text = '' OR subscriber_ref_kind || ':' || subscriber_ref = @subscriber_ref)
  AND (@status::text = '' OR status = @status)
ORDER BY updated_at DESC, id DESC
LIMIT @limit::integer
OFFSET @offset::bigint;
