-- name: response__create :exec
INSERT INTO interaction_hub_responses (
    id,
    request_id,
    response_action,
    responded_by_actor_ref,
    response_summary,
    response_object_uri,
    response_object_digest,
    response_object_size_bytes,
    source_kind,
    source_ref,
    owner_decision_ref,
    created_at
) VALUES (
    @id,
    @request_id,
    @response_action,
    @responded_by_actor_ref,
    @response_summary,
    @response_object_uri,
    @response_object_digest,
    @response_object_size_bytes,
    @source_kind,
    @source_ref,
    @owner_decision_ref,
    @created_at
);
