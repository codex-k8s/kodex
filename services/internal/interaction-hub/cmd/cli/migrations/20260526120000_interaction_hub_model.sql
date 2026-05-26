-- +goose Up
CREATE TABLE interaction_hub_threads (
    id uuid PRIMARY KEY,
    scope_type text NOT NULL,
    scope_ref text NOT NULL,
    thread_kind text NOT NULL,
    primary_actor_ref text NOT NULL DEFAULT '',
    source_kind text NOT NULL,
    source_ref text NOT NULL DEFAULT '',
    status text NOT NULL,
    latest_message_id uuid,
    correlation_id text NOT NULL,
    retention_class text NOT NULL,
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    closed_at timestamptz,
    CONSTRAINT interaction_hub_threads_scope_type_chk
        CHECK (scope_type IN ('platform', 'organization', 'project', 'repository', 'service')),
    CONSTRAINT interaction_hub_threads_scope_ref_chk CHECK (scope_ref <> ''),
    CONSTRAINT interaction_hub_threads_kind_chk
        CHECK (thread_kind IN ('user_dialog', 'owner_feedback', 'approval', 'human_gate', 'notification', 'ops')),
    CONSTRAINT interaction_hub_threads_source_kind_chk
        CHECK (source_kind IN ('web_console', 'voice', 'mcp', 'provider', 'channel_package', 'codex_hook', 'system', 'service')),
    CONSTRAINT interaction_hub_threads_status_chk
        CHECK (status IN ('open', 'waiting', 'closed', 'archived')),
    CONSTRAINT interaction_hub_threads_correlation_chk CHECK (correlation_id <> ''),
    CONSTRAINT interaction_hub_threads_retention_chk CHECK (retention_class <> ''),
    CONSTRAINT interaction_hub_threads_version_chk CHECK (version > 0)
);

CREATE INDEX interaction_hub_threads_scope_status_idx
    ON interaction_hub_threads (scope_type, scope_ref, status, updated_at DESC, id);

CREATE INDEX interaction_hub_threads_correlation_idx
    ON interaction_hub_threads (correlation_id);

CREATE TABLE interaction_hub_messages (
    id uuid PRIMARY KEY,
    thread_id uuid NOT NULL REFERENCES interaction_hub_threads(id),
    message_kind text NOT NULL,
    author_ref text NOT NULL,
    body_summary text NOT NULL DEFAULT '',
    body_object_uri text NOT NULL DEFAULT '',
    body_object_digest text NOT NULL DEFAULT '',
    body_object_size_bytes bigint,
    body_digest text NOT NULL DEFAULT '',
    locale text NOT NULL DEFAULT '',
    safe_metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL,
    CONSTRAINT interaction_hub_messages_kind_chk
        CHECK (message_kind IN ('user_text', 'voice_transcript', 'agent_text', 'system_notice', 'response_summary', 'callback_summary')),
    CONSTRAINT interaction_hub_messages_author_chk CHECK (author_ref <> ''),
    CONSTRAINT interaction_hub_messages_object_size_chk
        CHECK (body_object_size_bytes IS NULL OR body_object_size_bytes >= 0),
    CONSTRAINT interaction_hub_messages_metadata_chk CHECK (jsonb_typeof(safe_metadata) = 'object')
);

ALTER TABLE interaction_hub_threads
    ADD CONSTRAINT interaction_hub_threads_latest_message_fk
        FOREIGN KEY (latest_message_id)
        REFERENCES interaction_hub_messages(id);

CREATE INDEX interaction_hub_messages_thread_created_idx
    ON interaction_hub_messages (thread_id, created_at, id);

CREATE TABLE interaction_hub_subscriptions (
    id uuid PRIMARY KEY,
    scope_type text NOT NULL,
    scope_ref text NOT NULL,
    subscriber_ref_kind text NOT NULL,
    subscriber_ref text NOT NULL,
    event_filter jsonb NOT NULL DEFAULT '{}'::jsonb,
    delivery_preferences jsonb NOT NULL DEFAULT '{}'::jsonb,
    status text NOT NULL,
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT interaction_hub_subscriptions_scope_type_chk
        CHECK (scope_type IN ('platform', 'organization', 'project', 'repository', 'service')),
    CONSTRAINT interaction_hub_subscriptions_scope_ref_chk CHECK (scope_ref <> ''),
    CONSTRAINT interaction_hub_subscriptions_subscriber_kind_chk CHECK (subscriber_ref_kind <> ''),
    CONSTRAINT interaction_hub_subscriptions_subscriber_ref_chk CHECK (subscriber_ref <> ''),
    CONSTRAINT interaction_hub_subscriptions_event_filter_chk CHECK (jsonb_typeof(event_filter) = 'object'),
    CONSTRAINT interaction_hub_subscriptions_preferences_chk CHECK (jsonb_typeof(delivery_preferences) = 'object'),
    CONSTRAINT interaction_hub_subscriptions_status_chk CHECK (status IN ('active', 'paused', 'disabled')),
    CONSTRAINT interaction_hub_subscriptions_version_chk CHECK (version > 0)
);

CREATE INDEX interaction_hub_subscriptions_scope_status_idx
    ON interaction_hub_subscriptions (scope_type, scope_ref, status, updated_at DESC, id);

CREATE INDEX interaction_hub_subscriptions_subscriber_idx
    ON interaction_hub_subscriptions (subscriber_ref_kind, subscriber_ref, status);

CREATE INDEX interaction_hub_subscriptions_event_filter_gin_idx
    ON interaction_hub_subscriptions USING gin (event_filter);

CREATE TABLE interaction_hub_requests (
    id uuid PRIMARY KEY,
    request_kind text NOT NULL,
    scope_type text NOT NULL,
    scope_ref text NOT NULL,
    thread_id uuid REFERENCES interaction_hub_threads(id),
    source_owner_kind text NOT NULL,
    source_owner_ref text NOT NULL DEFAULT '',
    ingress_kind text NOT NULL,
    ingress_ref text NOT NULL DEFAULT '',
    decision_owner_kind text NOT NULL DEFAULT '',
    decision_owner_request_ref text NOT NULL DEFAULT '',
    decision_owner_decision_ref text NOT NULL DEFAULT '',
    target_refs jsonb NOT NULL DEFAULT '[]'::jsonb,
    context_refs jsonb NOT NULL DEFAULT '[]'::jsonb,
    prompt_summary text NOT NULL,
    prompt_object_uri text NOT NULL DEFAULT '',
    prompt_object_digest text NOT NULL DEFAULT '',
    prompt_object_size_bytes bigint,
    allowed_actions jsonb NOT NULL DEFAULT '[]'::jsonb,
    risk_class text NOT NULL DEFAULT '',
    status text NOT NULL,
    deadline_at timestamptz,
    reminder_policy_ref text NOT NULL DEFAULT '',
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    resolved_at timestamptz,
    CONSTRAINT interaction_hub_requests_kind_chk CHECK (request_kind IN ('feedback', 'approval', 'human_gate')),
    CONSTRAINT interaction_hub_requests_scope_type_chk
        CHECK (scope_type IN ('platform', 'organization', 'project', 'repository', 'service')),
    CONSTRAINT interaction_hub_requests_scope_ref_chk CHECK (scope_ref <> ''),
    CONSTRAINT interaction_hub_requests_source_owner_kind_chk
        CHECK (source_owner_kind IN ('agent_manager', 'slot_agent', 'governance_manager', 'provider_hub', 'operations_hub', 'user', 'system')),
    CONSTRAINT interaction_hub_requests_ingress_kind_chk
        CHECK (ingress_kind IN ('direct_grpc', 'mcp', 'codex_hook', 'gateway', 'system', 'service')),
    CONSTRAINT interaction_hub_requests_decision_owner_kind_chk
        CHECK (decision_owner_kind IN ('', 'agent_manager', 'governance_manager', 'provider_hub', 'operations_hub', 'system')),
    CONSTRAINT interaction_hub_requests_target_refs_chk CHECK (jsonb_typeof(target_refs) = 'array'),
    CONSTRAINT interaction_hub_requests_context_refs_chk CHECK (jsonb_typeof(context_refs) = 'array'),
    CONSTRAINT interaction_hub_requests_prompt_summary_chk CHECK (prompt_summary <> ''),
    CONSTRAINT interaction_hub_requests_prompt_object_size_chk
        CHECK (prompt_object_size_bytes IS NULL OR prompt_object_size_bytes >= 0),
    CONSTRAINT interaction_hub_requests_allowed_actions_chk CHECK (jsonb_typeof(allowed_actions) = 'array'),
    CONSTRAINT interaction_hub_requests_risk_class_chk
        CHECK (risk_class IN ('', 'low', 'medium', 'high', 'critical')),
    CONSTRAINT interaction_hub_requests_status_chk
        CHECK (status IN ('created', 'routed', 'waiting', 'answered', 'expired', 'cancelled', 'failed')),
    CONSTRAINT interaction_hub_requests_version_chk CHECK (version > 0)
);

CREATE INDEX interaction_hub_requests_scope_status_deadline_idx
    ON interaction_hub_requests (scope_type, scope_ref, status, deadline_at, id);

CREATE INDEX interaction_hub_requests_source_owner_idx
    ON interaction_hub_requests (source_owner_kind, source_owner_ref);

CREATE INDEX interaction_hub_requests_thread_idx
    ON interaction_hub_requests (thread_id, created_at DESC, id)
    WHERE thread_id IS NOT NULL;

CREATE TABLE interaction_hub_responses (
    id uuid PRIMARY KEY,
    request_id uuid NOT NULL REFERENCES interaction_hub_requests(id),
    response_action text NOT NULL,
    responded_by_actor_ref text NOT NULL,
    response_summary text NOT NULL DEFAULT '',
    response_object_uri text NOT NULL DEFAULT '',
    response_object_digest text NOT NULL DEFAULT '',
    response_object_size_bytes bigint,
    source_kind text NOT NULL,
    source_ref text NOT NULL DEFAULT '',
    owner_decision_ref text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL,
    UNIQUE (request_id),
    CONSTRAINT interaction_hub_responses_action_chk
        CHECK (response_action IN ('answer', 'approve', 'reject', 'defer', 'acknowledge', 'custom')),
    CONSTRAINT interaction_hub_responses_actor_chk CHECK (responded_by_actor_ref <> ''),
    CONSTRAINT interaction_hub_responses_object_size_chk
        CHECK (response_object_size_bytes IS NULL OR response_object_size_bytes >= 0),
    CONSTRAINT interaction_hub_responses_source_kind_chk
        CHECK (source_kind IN ('web_console', 'mcp', 'channel_callback', 'system', 'service'))
);

CREATE TABLE interaction_hub_notifications (
    id uuid PRIMARY KEY,
    scope_type text NOT NULL,
    scope_ref text NOT NULL,
    notification_kind text NOT NULL,
    request_id uuid REFERENCES interaction_hub_requests(id),
    subscription_id uuid REFERENCES interaction_hub_subscriptions(id),
    recipient_refs jsonb NOT NULL DEFAULT '[]'::jsonb,
    message_template_ref text NOT NULL,
    message_summary text NOT NULL,
    priority text NOT NULL,
    status text NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    expires_at timestamptz,
    CONSTRAINT interaction_hub_notifications_scope_type_chk
        CHECK (scope_type IN ('platform', 'organization', 'project', 'repository', 'service')),
    CONSTRAINT interaction_hub_notifications_scope_ref_chk CHECK (scope_ref <> ''),
    CONSTRAINT interaction_hub_notifications_kind_chk
        CHECK (notification_kind IN ('status', 'reminder', 'error', 'attention', 'decision_required', 'ops')),
    CONSTRAINT interaction_hub_notifications_recipient_refs_chk CHECK (jsonb_typeof(recipient_refs) = 'array'),
    CONSTRAINT interaction_hub_notifications_template_chk CHECK (message_template_ref <> ''),
    CONSTRAINT interaction_hub_notifications_message_chk CHECK (message_summary <> ''),
    CONSTRAINT interaction_hub_notifications_priority_chk CHECK (priority IN ('low', 'normal', 'high', 'urgent')),
    CONSTRAINT interaction_hub_notifications_status_chk
        CHECK (status IN ('created', 'queued', 'delivered', 'acknowledged', 'expired', 'failed'))
);

CREATE INDEX interaction_hub_notifications_scope_status_idx
    ON interaction_hub_notifications (scope_type, scope_ref, status, updated_at DESC, id);

CREATE TABLE interaction_hub_delivery_routes (
    id uuid PRIMARY KEY,
    scope_type text NOT NULL,
    scope_ref text NOT NULL,
    surface_kind text NOT NULL,
    channel_capability_ref text NOT NULL DEFAULT '',
    package_installation_ref text NOT NULL DEFAULT '',
    routing_policy_ref text NOT NULL DEFAULT '',
    status text NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT interaction_hub_delivery_routes_scope_type_chk
        CHECK (scope_type IN ('platform', 'organization', 'project', 'repository', 'service')),
    CONSTRAINT interaction_hub_delivery_routes_scope_ref_chk CHECK (scope_ref <> ''),
    CONSTRAINT interaction_hub_delivery_routes_surface_kind_chk
        CHECK (surface_kind IN ('web_console', 'voice', 'provider_surface', 'channel_package', 'system')),
    CONSTRAINT interaction_hub_delivery_routes_status_chk CHECK (status IN ('active', 'paused', 'disabled'))
);

CREATE INDEX interaction_hub_delivery_routes_scope_status_idx
    ON interaction_hub_delivery_routes (scope_type, scope_ref, status, surface_kind, id);

CREATE TABLE interaction_hub_delivery_attempts (
    id uuid PRIMARY KEY,
    request_id uuid REFERENCES interaction_hub_requests(id),
    notification_id uuid REFERENCES interaction_hub_notifications(id),
    route_id uuid NOT NULL REFERENCES interaction_hub_delivery_routes(id),
    delivery_id text NOT NULL,
    delivery_kind text NOT NULL,
    status text NOT NULL,
    channel_message_ref text NOT NULL DEFAULT '',
    attempt_number integer NOT NULL,
    next_retry_at timestamptz,
    error_code text NOT NULL DEFAULT '',
    error_class text NOT NULL DEFAULT '',
    payload_digest text NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    sent_at timestamptz,
    CONSTRAINT interaction_hub_delivery_attempts_single_target_chk
        CHECK ((request_id IS NOT NULL) <> (notification_id IS NOT NULL)),
    CONSTRAINT interaction_hub_delivery_attempts_delivery_id_chk CHECK (delivery_id <> ''),
    CONSTRAINT interaction_hub_delivery_attempts_kind_chk
        CHECK (delivery_kind IN ('feedback', 'approval', 'human_gate', 'notification')),
    CONSTRAINT interaction_hub_delivery_attempts_status_chk
        CHECK (status IN ('queued', 'sent', 'accepted', 'delivered', 'failed', 'cancelled', 'expired')),
    CONSTRAINT interaction_hub_delivery_attempts_number_chk CHECK (attempt_number > 0),
    CONSTRAINT interaction_hub_delivery_attempts_error_class_chk
        CHECK (error_class IN ('', 'temporary', 'permanent', 'auth', 'rate_limited', 'policy')),
    CONSTRAINT interaction_hub_delivery_attempts_payload_digest_chk CHECK (payload_digest <> '')
);

CREATE UNIQUE INDEX interaction_hub_delivery_attempts_delivery_id_uidx
    ON interaction_hub_delivery_attempts (delivery_id);

CREATE INDEX interaction_hub_delivery_attempts_retry_idx
    ON interaction_hub_delivery_attempts (status, next_retry_at, route_id)
    WHERE status IN ('queued', 'failed');

CREATE INDEX interaction_hub_delivery_attempts_request_idx
    ON interaction_hub_delivery_attempts (request_id, created_at DESC, id)
    WHERE request_id IS NOT NULL;

CREATE INDEX interaction_hub_delivery_attempts_notification_idx
    ON interaction_hub_delivery_attempts (notification_id, created_at DESC, id)
    WHERE notification_id IS NOT NULL;

CREATE TABLE interaction_hub_channel_callbacks (
    id uuid PRIMARY KEY,
    callback_id text NOT NULL,
    delivery_attempt_id uuid REFERENCES interaction_hub_delivery_attempts(id),
    request_id uuid REFERENCES interaction_hub_requests(id),
    source_route_id uuid REFERENCES interaction_hub_delivery_routes(id),
    actor_ref text NOT NULL DEFAULT '',
    action text NOT NULL DEFAULT '',
    callback_summary text NOT NULL DEFAULT '',
    callback_object_uri text NOT NULL DEFAULT '',
    callback_object_digest text NOT NULL DEFAULT '',
    callback_object_size_bytes bigint,
    signature_status text NOT NULL,
    processing_status text NOT NULL,
    error_code text NOT NULL DEFAULT '',
    received_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL,
    CONSTRAINT interaction_hub_channel_callbacks_callback_id_chk CHECK (callback_id <> ''),
    CONSTRAINT interaction_hub_channel_callbacks_object_size_chk
        CHECK (callback_object_size_bytes IS NULL OR callback_object_size_bytes >= 0),
    CONSTRAINT interaction_hub_channel_callbacks_signature_chk
        CHECK (signature_status IN ('verified', 'trusted_internal', 'rejected_before_domain')),
    CONSTRAINT interaction_hub_channel_callbacks_processing_chk
        CHECK (processing_status IN ('accepted', 'duplicate', 'rejected', 'failed'))
);

CREATE UNIQUE INDEX interaction_hub_channel_callbacks_callback_id_uidx
    ON interaction_hub_channel_callbacks (callback_id);

CREATE INDEX interaction_hub_channel_callbacks_request_idx
    ON interaction_hub_channel_callbacks (request_id, created_at DESC, id)
    WHERE request_id IS NOT NULL;

CREATE TABLE interaction_hub_command_results (
    key text PRIMARY KEY,
    command_id uuid,
    idempotency_key text NOT NULL DEFAULT '',
    actor_ref text NOT NULL,
    operation text NOT NULL,
    aggregate_type text NOT NULL,
    aggregate_id uuid NOT NULL,
    request_fingerprint text NOT NULL,
    result_payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL,
    CONSTRAINT interaction_hub_command_results_key_chk CHECK (key <> ''),
    CONSTRAINT interaction_hub_command_results_actor_ref_chk CHECK (actor_ref <> ''),
    CONSTRAINT interaction_hub_command_results_operation_chk CHECK (operation <> ''),
    CONSTRAINT interaction_hub_command_results_aggregate_type_chk
        CHECK (aggregate_type IN ('thread', 'message', 'request', 'response', 'notification', 'subscription', 'route', 'delivery', 'callback')),
    CONSTRAINT interaction_hub_command_results_fingerprint_chk CHECK (request_fingerprint <> ''),
    CONSTRAINT interaction_hub_command_results_payload_chk CHECK (jsonb_typeof(result_payload) = 'object'),
    CONSTRAINT interaction_hub_command_results_identity_chk CHECK (command_id IS NOT NULL OR idempotency_key <> '')
);

CREATE UNIQUE INDEX interaction_hub_command_results_command_id_uidx
    ON interaction_hub_command_results (command_id)
    WHERE command_id IS NOT NULL;

CREATE UNIQUE INDEX interaction_hub_command_results_idempotency_uidx
    ON interaction_hub_command_results (operation, actor_ref, idempotency_key)
    WHERE idempotency_key <> '';

CREATE INDEX interaction_hub_command_results_aggregate_idx
    ON interaction_hub_command_results (aggregate_type, aggregate_id);

CREATE TABLE interaction_hub_outbox_events (
    id uuid PRIMARY KEY,
    event_type text NOT NULL,
    schema_version integer NOT NULL,
    aggregate_type text NOT NULL,
    aggregate_id uuid NOT NULL,
    payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    occurred_at timestamptz NOT NULL,
    published_at timestamptz,
    attempt_count integer NOT NULL DEFAULT 0,
    next_attempt_at timestamptz NOT NULL DEFAULT '1970-01-01 00:00:00+00'::timestamptz,
    locked_until timestamptz,
    failed_permanently_at timestamptz,
    failure_kind text NOT NULL DEFAULT '',
    last_error text NOT NULL DEFAULT '',
    CONSTRAINT interaction_hub_outbox_events_type_chk CHECK (event_type LIKE 'interaction.%'),
    CONSTRAINT interaction_hub_outbox_events_schema_version_chk CHECK (schema_version > 0),
    CONSTRAINT interaction_hub_outbox_events_aggregate_type_chk
        CHECK (aggregate_type IN ('thread', 'message', 'request', 'response', 'notification', 'subscription', 'route', 'delivery', 'callback')),
    CONSTRAINT interaction_hub_outbox_events_payload_chk CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT interaction_hub_outbox_events_attempt_count_chk CHECK (attempt_count >= 0),
    CONSTRAINT interaction_hub_outbox_events_failure_kind_chk
        CHECK (failure_kind IN ('', 'retryable', 'permanent'))
);

CREATE INDEX interaction_hub_outbox_events_ready_idx
    ON interaction_hub_outbox_events (published_at, failed_permanently_at, next_attempt_at, locked_until, occurred_at);

-- +goose Down
DROP TABLE IF EXISTS interaction_hub_outbox_events;
DROP TABLE IF EXISTS interaction_hub_command_results;
DROP TABLE IF EXISTS interaction_hub_channel_callbacks;
DROP TABLE IF EXISTS interaction_hub_delivery_attempts;
DROP TABLE IF EXISTS interaction_hub_delivery_routes;
DROP TABLE IF EXISTS interaction_hub_notifications;
DROP TABLE IF EXISTS interaction_hub_responses;
DROP TABLE IF EXISTS interaction_hub_requests;
DROP TABLE IF EXISTS interaction_hub_subscriptions;

ALTER TABLE interaction_hub_threads
    DROP CONSTRAINT IF EXISTS interaction_hub_threads_latest_message_fk;

DROP TABLE IF EXISTS interaction_hub_messages;
DROP TABLE IF EXISTS interaction_hub_threads;
