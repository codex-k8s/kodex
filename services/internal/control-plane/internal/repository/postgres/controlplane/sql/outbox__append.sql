INSERT INTO control_plane.outbox_events (
    event_id,
    organization_id,
    project_id,
    event_name,
    aggregate_type,
    aggregate_id,
    aggregate_version,
    event_sequence,
    correlation_id,
    causation_id,
    envelope,
    occurred_at,
    available_at
) VALUES (
    @event_id::uuid,
    @organization_id::uuid,
    @project_id::uuid,
    @event_name,
    @aggregate_type,
    @aggregate_id::uuid,
    @aggregate_version,
    @event_sequence,
    @correlation_id::uuid,
    nullif(@causation_id, '')::uuid,
    @envelope::jsonb,
    @occurred_at,
    @occurred_at
)
