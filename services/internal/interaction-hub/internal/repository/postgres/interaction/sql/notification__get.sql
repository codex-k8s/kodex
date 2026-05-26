-- name: notification__get :one
SELECT
    id,
    scope_type,
    scope_ref,
    notification_kind,
    request_id,
    subscription_id,
    recipient_refs,
    message_template_ref,
    message_summary,
    priority,
    status,
    created_at,
    updated_at,
    expires_at,
    source_owner_kind,
    source_owner_ref,
    ingress_kind,
    ingress_ref,
    context_refs,
    channel_hint_refs,
    notification_policy_ref,
    message_title,
    body_preview
FROM interaction_hub_notifications
WHERE id = @id;
