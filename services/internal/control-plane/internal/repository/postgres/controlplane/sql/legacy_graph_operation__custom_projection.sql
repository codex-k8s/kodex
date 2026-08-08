-- name: GetLegacyCustomOperationProjection :one
WITH receipt AS (
    SELECT *
    FROM control_plane.legacy_graph_operation_receipts
    WHERE plan_id = @plan_id::uuid AND ordinal = @ordinal
), provenance AS (
    SELECT *
    FROM control_plane.legacy_graph_provenance
    WHERE plan_id = @plan_id::uuid AND ordinal = @ordinal
)
SELECT coalesce((CASE receipt.target_kind
    WHEN 'TURN_ATTEMPT' THEN (
        SELECT jsonb_build_object(
            'target', jsonb_build_object(
                'turn_id', attempt.turn_id::text,
                'attempt', attempt.attempt,
                'workload_id', attempt.workload_id,
                'authority_generation', attempt.authority_generation,
                'state', attempt.state,
                'input_sha256', attempt.input_sha256,
                'lease_fence', attempt.lease_fence,
                'started_at_unix_micro', (extract(epoch FROM attempt.started_at) * 1000000)::bigint,
                'finished_at_unix_micro', CASE WHEN attempt.finished_at IS NULL THEN NULL
                    ELSE (extract(epoch FROM attempt.finished_at) * 1000000)::bigint END,
                'outcome', attempt.outcome,
                'runtime_revision_id', attempt.runtime_revision_id::text,
                'runtime_revision_version', attempt.runtime_revision_version
            ),
            'provenance', to_jsonb(provenance)
        )
        FROM control_plane.turn_attempts AS attempt
        WHERE attempt.turn_id = provenance.root_turn_id
          AND attempt.attempt = provenance.root_attempt
    )
    WHEN 'DELEGATION_EDGE' THEN (
        SELECT jsonb_build_object(
            'target', jsonb_build_object(
                'id', edge.id::text,
                'organization_id', edge.organization_id::text,
                'project_id', edge.project_id::text,
                'parent_process_run_id', edge.parent_process_run_id::text,
                'source_session_id', edge.source_session_id::text,
                'source_turn_id', edge.source_turn_id::text,
                'source_attempt', edge.source_attempt,
                'source_input_sha256', edge.source_input_sha256,
                'target_session_id', edge.target_session_id::text,
                'target_role_id', edge.target_role_id::text,
                'target_turn_id', edge.target_turn_id::text,
                'target_attempt', edge.target_attempt,
                'target_input_sha256', edge.target_input_sha256,
                'root_initiator_actor_id', edge.root_initiator_actor_id::text,
                'grant_generation', edge.grant_generation,
                'created_at_unix_micro', (extract(epoch FROM edge.created_at) * 1000000)::bigint
            ),
            'provenance', to_jsonb(provenance)
        )
        FROM control_plane.delegation_edges AS edge
        WHERE edge.id = receipt.target_id
    )
    WHEN 'CALLBACK_MANIFEST' THEN (
        SELECT jsonb_build_object(
            'target', jsonb_build_object(
                'id', manifest.id::text,
                'plan_id', manifest.plan_id::text,
                'delegation_id', manifest.delegation_id::text,
                'callback_process_id', manifest.callback_process_id::text,
                'destinations', manifest.destinations,
                'manifest_sha256', manifest.manifest_sha256,
                'created_at_unix_micro', (extract(epoch FROM manifest.created_at) * 1000000)::bigint
            ),
            'provenance', to_jsonb(provenance)
        )
        FROM control_plane.delegation_callback_manifests AS manifest
        WHERE manifest.id = receipt.target_id AND manifest.plan_id = receipt.plan_id
    )
    WHEN 'CALLBACK_DELIVERY' THEN (
        SELECT jsonb_build_object(
            'target', jsonb_build_object(
                'id', delivery.id::text,
                'plan_id', delivery.plan_id::text,
                'manifest_id', delivery.manifest_id::text,
                'destination', delivery.destination,
                'receipt_sha256', delivery.receipt_sha256,
                'state', delivery.state,
                'delivered_at_unix_micro', (extract(epoch FROM delivery.delivered_at) * 1000000)::bigint
            ),
            'provenance', to_jsonb(provenance)
        )
        FROM control_plane.delegation_callback_deliveries AS delivery
        WHERE delivery.id = receipt.target_id AND delivery.plan_id = receipt.plan_id
    )
    ELSE NULL
END)::text, '')
FROM receipt
JOIN provenance
  ON provenance.plan_id = receipt.plan_id
 AND provenance.ordinal = receipt.ordinal
