-- name: DiagnosticsGet
SELECT
    schema_version,
    pending_outbox_events,
    terminal_outbox_events,
    oldest_pending_seconds,
    active_turn_leases,
    queued_schedule_occurrences,
    runtime_principal_status,
    runtime_principal_generation
FROM control_plane.safe_diagnostics()
