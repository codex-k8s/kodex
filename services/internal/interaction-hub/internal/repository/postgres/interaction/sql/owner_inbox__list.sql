-- name: owner_inbox__list :many
SELECT
    r.id,
    r.request_kind,
    r.scope_type,
    r.scope_ref,
    r.thread_id,
    r.source_owner_kind,
    r.source_owner_ref,
    r.ingress_kind,
    r.ingress_ref,
    r.decision_owner_kind,
    r.decision_owner_request_ref,
    r.decision_owner_decision_ref,
    r.target_refs,
    r.context_refs,
    r.prompt_summary,
    r.prompt_object_uri,
    r.prompt_object_digest,
    r.prompt_object_size_bytes,
    r.allowed_actions,
    r.risk_class,
    r.status,
    r.deadline_at,
    r.reminder_policy_ref,
    r.version,
    r.created_at,
    r.updated_at,
    r.resolved_at,
    COALESCE(delivery.attempt_count, 0)::integer AS delivery_attempt_count,
    delivery.latest_attempt_id,
    COALESCE(delivery.latest_delivery_id, '') AS latest_delivery_id,
    COALESCE(delivery.latest_status, '') AS latest_delivery_status,
    COALESCE(delivery.latest_error_code, '') AS latest_delivery_error_code,
    COALESCE(delivery.latest_error_class, '') AS latest_delivery_error_class,
    delivery.latest_next_retry_at,
    delivery.latest_updated_at,
    delivery.latest_route_id,
    COALESCE(delivery.latest_channel_message_ref, '') AS latest_channel_message_ref,
    response.id AS response_id,
    response.response_action,
    response.responded_by_actor_ref,
    response.response_summary,
    response.response_object_uri,
    response.response_object_digest,
    response.response_object_size_bytes,
    response.source_kind AS response_source_kind,
    response.source_ref AS response_source_ref,
    response.owner_decision_ref,
    response.created_at AS response_created_at,
    callback.id AS callback_ref,
    callback.callback_id,
    callback.delivery_id AS callback_delivery_id,
    callback.actor_ref AS callback_actor_ref,
    callback.action AS callback_action,
    callback.signature_status,
    callback.processing_status,
    callback.error_code AS callback_error_code,
    callback.received_at AS callback_received_at,
    callback.gateway_ref,
    callback.correlation_id
FROM interaction_hub_requests r
LEFT JOIN LATERAL (
    SELECT
        cb.id,
        cb.callback_id,
        cb.delivery_id,
        cb.actor_ref,
        cb.action,
        cb.signature_status,
        cb.processing_status,
        cb.error_code,
        cb.received_at,
        cb.gateway_ref,
        cb.correlation_id
    FROM interaction_hub_channel_callbacks cb
    WHERE cb.request_id = r.id
    ORDER BY cb.created_at DESC, cb.id DESC
    LIMIT 1
) callback ON true
LEFT JOIN LATERAL (
    SELECT
        resp.id,
        resp.response_action,
        resp.responded_by_actor_ref,
        resp.response_summary,
        resp.response_object_uri,
        resp.response_object_digest,
        resp.response_object_size_bytes,
        resp.source_kind,
        resp.source_ref,
        resp.owner_decision_ref,
        resp.created_at
    FROM interaction_hub_responses resp
    WHERE resp.request_id = r.id
    ORDER BY resp.created_at DESC, resp.id DESC
    LIMIT 1
) response ON true
LEFT JOIN LATERAL (
    SELECT
        COUNT(*)::integer AS attempt_count,
        (ARRAY_AGG(da.id ORDER BY da.attempt_number DESC, da.created_at DESC, da.id DESC))[1] AS latest_attempt_id,
        (ARRAY_AGG(da.delivery_id ORDER BY da.attempt_number DESC, da.created_at DESC, da.id DESC))[1] AS latest_delivery_id,
        (ARRAY_AGG(da.status ORDER BY da.attempt_number DESC, da.created_at DESC, da.id DESC))[1] AS latest_status,
        (ARRAY_AGG(da.error_code ORDER BY da.attempt_number DESC, da.created_at DESC, da.id DESC))[1] AS latest_error_code,
        (ARRAY_AGG(da.error_class ORDER BY da.attempt_number DESC, da.created_at DESC, da.id DESC))[1] AS latest_error_class,
        (ARRAY_AGG(da.next_retry_at ORDER BY da.attempt_number DESC, da.created_at DESC, da.id DESC))[1] AS latest_next_retry_at,
        (ARRAY_AGG(da.updated_at ORDER BY da.attempt_number DESC, da.created_at DESC, da.id DESC))[1] AS latest_updated_at,
        (ARRAY_AGG(da.route_id ORDER BY da.attempt_number DESC, da.created_at DESC, da.id DESC))[1] AS latest_route_id,
        (ARRAY_AGG(da.channel_message_ref ORDER BY da.attempt_number DESC, da.created_at DESC, da.id DESC))[1] AS latest_channel_message_ref
    FROM interaction_hub_delivery_attempts da
    WHERE da.request_id = r.id
) delivery ON true
WHERE r.scope_type = @scope_type
  AND r.scope_ref = @scope_ref
  AND (@request_id::uuid IS NULL OR r.id = @request_id::uuid)
  AND (cardinality(@request_kinds::text[]) = 0 OR r.request_kind = ANY(@request_kinds::text[]))
  AND (
      (cardinality(@statuses::text[]) = 0 AND r.status = ANY(@default_statuses::text[]))
      OR (cardinality(@statuses::text[]) > 0 AND r.status = ANY(@statuses::text[]))
      OR (@include_diagnostics::boolean AND callback.processing_status = 'rejected')
  )
  AND (@source_owner_kind::text = '' OR r.source_owner_kind = @source_owner_kind)
  AND (@source_owner_ref::text = '' OR r.source_owner_ref = @source_owner_ref)
  AND (@assignee_ref::jsonb = '[]'::jsonb OR r.target_refs @> @assignee_ref::jsonb)
  AND (
      @actor_ref::text = ''
      OR response.responded_by_actor_ref = @actor_ref
      OR callback.actor_ref = @actor_ref
  )
  AND (@correlation_ref::jsonb = '[]'::jsonb OR r.context_refs @> @correlation_ref::jsonb)
  AND (
      @correlation_id::text = ''
      OR r.source_owner_ref = @correlation_id
      OR r.ingress_ref = @correlation_id
      OR callback.correlation_id = @correlation_id
  )
ORDER BY
    CASE WHEN r.status = ANY(@default_statuses::text[]) THEN 0 ELSE 1 END,
    CASE WHEN r.deadline_at IS NULL THEN 1 ELSE 0 END,
    r.deadline_at ASC,
    r.updated_at DESC,
    r.id DESC
LIMIT @limit::integer
OFFSET @offset::bigint;
