WITH receipt AS (
    SELECT * FROM control_plane.legacy_graph_operation_receipts
    WHERE plan_id = @plan_id::uuid AND ordinal = @ordinal
), plan AS (
    SELECT * FROM control_plane.legacy_graph_migration_plans
    WHERE plan_id = @plan_id::uuid
), evidence AS (
    SELECT
        cardinality(receipt.audit_ids) = 1
        AND EXISTS (
            SELECT 1 FROM control_plane.audit_events AS audit
            WHERE audit.id = receipt.audit_ids[1]
              AND audit.resource_id = receipt.target_id
              AND audit.resource_kind = receipt.target_kind
              AND audit.resource_version = receipt.target_version
              AND audit.organization_id = plan.organization_id
              AND audit.project_id = plan.project_id
              AND audit.actor_id = plan.owner_actor_id
              AND audit.action = 'materialize_legacy_' || lower(receipt.target_kind)
              AND audit.outcome = 'succeeded'
              AND audit.occurred_at = receipt.materialized_at
        ) AS audit_ok,
        cardinality(receipt.event_ids) = CASE
            WHEN @event_required::boolean THEN 1 ELSE 0 END
        AND NOT EXISTS (
            SELECT 1 FROM unnest(receipt.event_ids, receipt.event_sequences)
                AS expected(event_id, event_sequence)
            WHERE NOT EXISTS (
                SELECT 1 FROM control_plane.outbox_events AS event
                WHERE event.event_id = expected.event_id
                  AND event.aggregate_id = receipt.target_id
                  AND event.aggregate_type = receipt.target_kind
                  AND event.aggregate_version = receipt.target_version
                  AND event.event_sequence = expected.event_sequence
                  AND event.event_name = @event_name
                  AND event.organization_id = plan.organization_id
                  AND event.project_id = plan.project_id
                  AND event.correlation_id = (
                      SELECT audit.correlation_id
                      FROM control_plane.audit_events AS audit
                      WHERE audit.id = receipt.audit_ids[1]
                  )
                  AND event.causation_id IS NULL
                  AND event.occurred_at = receipt.materialized_at
                  AND event.available_at = event.occurred_at
                  AND jsonb_typeof(event.envelope -> 'occurredAt') = 'string'
                  AND event.envelope - 'occurredAt' = jsonb_build_object(
                      'eventId', event.event_id::text,
                      'eventName', event.event_name,
                      'eventVersion', 1,
                      'schemaVersion', 1,
                      'aggregateType', receipt.target_kind,
                      'aggregateId', receipt.target_id::text,
                      'aggregateVersion', receipt.target_version,
                      'eventSequence', expected.event_sequence,
                      'correlationId', event.correlation_id::text,
                      'organizationId', plan.organization_id::text,
                      'data', jsonb_build_object(
                          'projectId', plan.project_id::text,
                          'resourceId', receipt.target_id::text,
                          'resourceKind', receipt.target_kind,
                          'resourceState', receipt.target_state,
                          'resourceVersion', receipt.target_version
                      )
                  )
            )
        ) AS events_ok,
        EXISTS (
            SELECT 1 FROM control_plane.legacy_graph_provenance AS provenance
            WHERE provenance.plan_id = receipt.plan_id
              AND provenance.ordinal = receipt.ordinal
              AND provenance.target_id = receipt.target_id
              AND provenance.target_kind = receipt.target_kind
              AND provenance.lineage_sha256 = receipt.provenance_sha256
              AND provenance.root_actor_id = plan.owner_actor_id
              AND EXISTS (
                  SELECT 1
                  FROM control_plane.legacy_graph_source_dispositions AS source
                  WHERE source.plan_id = provenance.plan_id
                    AND source.source_table = provenance.source_table
                    AND source.disposition = 'MATERIALIZE'
              )
        ) AS provenance_ok,
        CASE receipt.target_kind
          WHEN 'TURN_ATTEMPT' THEN EXISTS (
              SELECT 1
              FROM control_plane.legacy_graph_provenance AS provenance
              JOIN control_plane.turn_attempts AS attempt
                ON attempt.turn_id = provenance.root_turn_id
               AND attempt.attempt = provenance.root_attempt
               AND attempt.runtime_revision_id = provenance.runtime_revision_id
               AND attempt.runtime_revision_version = provenance.runtime_revision_version
              JOIN control_plane.resources AS turn
                ON turn.id = attempt.turn_id
               AND turn.organization_id = plan.organization_id
               AND turn.project_id = plan.project_id
              WHERE provenance.plan_id = receipt.plan_id
                AND provenance.ordinal = receipt.ordinal
                AND attempt.input_sha256 = provenance.immutable_input_sha256
          )
          WHEN 'DELEGATION_EDGE' THEN EXISTS (
              SELECT 1 FROM control_plane.delegation_edges AS edge
              WHERE edge.id = receipt.target_id
                AND edge.organization_id = plan.organization_id
                AND edge.project_id = plan.project_id
          )
          WHEN 'CALLBACK_MANIFEST' THEN EXISTS (
              SELECT 1 FROM control_plane.delegation_callback_manifests AS manifest
              WHERE manifest.id = receipt.target_id AND manifest.plan_id = receipt.plan_id
          )
          WHEN 'CALLBACK_DELIVERY' THEN EXISTS (
              SELECT 1 FROM control_plane.delegation_callback_deliveries AS delivery
              WHERE delivery.id = receipt.target_id AND delivery.plan_id = receipt.plan_id
          )
          ELSE EXISTS (
              SELECT 1 FROM control_plane.resources AS resource
              WHERE resource.id = receipt.target_id
                AND resource.kind = receipt.target_kind
                AND resource.version = receipt.target_version
                AND resource.state = receipt.target_state
                AND resource.organization_id = plan.organization_id
                AND resource.project_id = plan.project_id
                AND resource.owner_actor_id = plan.owner_actor_id
          )
        END AS target_ok
    FROM receipt
    JOIN plan ON plan.plan_id = receipt.plan_id
)
SELECT audit_ok, events_ok, provenance_ok, target_ok
FROM evidence
