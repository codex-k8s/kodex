-- +goose Up
ALTER TABLE interaction_hub_notifications
    ADD COLUMN source_owner_kind text NOT NULL DEFAULT 'system',
    ADD COLUMN source_owner_ref text NOT NULL DEFAULT '',
    ADD COLUMN ingress_kind text NOT NULL DEFAULT 'service',
    ADD COLUMN ingress_ref text NOT NULL DEFAULT '',
    ADD COLUMN context_refs jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN channel_hint_refs jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN notification_policy_ref text NOT NULL DEFAULT '',
    ADD COLUMN message_title text NOT NULL DEFAULT '',
    ADD COLUMN body_preview text NOT NULL DEFAULT '',
    ADD CONSTRAINT interaction_hub_notifications_source_owner_kind_chk
        CHECK (source_owner_kind IN ('agent_manager', 'slot_agent', 'governance_manager', 'provider_hub', 'operations_hub', 'user', 'system')),
    ADD CONSTRAINT interaction_hub_notifications_ingress_kind_chk
        CHECK (ingress_kind IN ('direct_grpc', 'mcp', 'codex_hook', 'gateway', 'system', 'service')),
    ADD CONSTRAINT interaction_hub_notifications_context_refs_chk CHECK (jsonb_typeof(context_refs) = 'array'),
    ADD CONSTRAINT interaction_hub_notifications_channel_hints_chk CHECK (jsonb_typeof(channel_hint_refs) = 'array');

CREATE INDEX interaction_hub_notifications_source_owner_idx
    ON interaction_hub_notifications (source_owner_kind, source_owner_ref, created_at DESC, id);

CREATE INDEX interaction_hub_notifications_request_idx
    ON interaction_hub_notifications (request_id, created_at DESC, id)
    WHERE request_id IS NOT NULL;

CREATE INDEX interaction_hub_notifications_subscription_idx
    ON interaction_hub_notifications (subscription_id, created_at DESC, id)
    WHERE subscription_id IS NOT NULL;

ALTER TABLE interaction_hub_subscriptions
    ADD COLUMN source_owner_kind text NOT NULL DEFAULT 'system',
    ADD COLUMN source_owner_ref text NOT NULL DEFAULT '',
    ADD COLUMN channel_hint_refs jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN subscription_policy_ref text NOT NULL DEFAULT '',
    ADD CONSTRAINT interaction_hub_subscriptions_source_owner_kind_chk
        CHECK (source_owner_kind IN ('agent_manager', 'slot_agent', 'governance_manager', 'provider_hub', 'operations_hub', 'user', 'system')),
    ADD CONSTRAINT interaction_hub_subscriptions_channel_hints_chk CHECK (jsonb_typeof(channel_hint_refs) = 'array');

CREATE INDEX interaction_hub_subscriptions_source_owner_idx
    ON interaction_hub_subscriptions (source_owner_kind, source_owner_ref, updated_at DESC, id);

-- +goose Down
DROP INDEX IF EXISTS interaction_hub_subscriptions_source_owner_idx;

ALTER TABLE interaction_hub_subscriptions
    DROP CONSTRAINT IF EXISTS interaction_hub_subscriptions_channel_hints_chk,
    DROP CONSTRAINT IF EXISTS interaction_hub_subscriptions_source_owner_kind_chk,
    DROP COLUMN IF EXISTS subscription_policy_ref,
    DROP COLUMN IF EXISTS channel_hint_refs,
    DROP COLUMN IF EXISTS source_owner_ref,
    DROP COLUMN IF EXISTS source_owner_kind;

DROP INDEX IF EXISTS interaction_hub_notifications_subscription_idx;
DROP INDEX IF EXISTS interaction_hub_notifications_request_idx;
DROP INDEX IF EXISTS interaction_hub_notifications_source_owner_idx;

ALTER TABLE interaction_hub_notifications
    DROP CONSTRAINT IF EXISTS interaction_hub_notifications_channel_hints_chk,
    DROP CONSTRAINT IF EXISTS interaction_hub_notifications_context_refs_chk,
    DROP CONSTRAINT IF EXISTS interaction_hub_notifications_ingress_kind_chk,
    DROP CONSTRAINT IF EXISTS interaction_hub_notifications_source_owner_kind_chk,
    DROP COLUMN IF EXISTS body_preview,
    DROP COLUMN IF EXISTS message_title,
    DROP COLUMN IF EXISTS notification_policy_ref,
    DROP COLUMN IF EXISTS channel_hint_refs,
    DROP COLUMN IF EXISTS context_refs,
    DROP COLUMN IF EXISTS ingress_ref,
    DROP COLUMN IF EXISTS ingress_kind,
    DROP COLUMN IF EXISTS source_owner_ref,
    DROP COLUMN IF EXISTS source_owner_kind;
