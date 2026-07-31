-- name: OutboxTerminalRepair
SELECT *
FROM control_plane.repair_terminal_outbox_event(
    @event_id::uuid,
    @expected_sequence,
    @expected_attempts,
    @idempotency_key_hash,
    @request_hash,
    @reason_code,
    @evidence_sha256,
    @actor_id::uuid,
    @correlation_id::uuid,
    @policy_revision,
    @repaired_at
)
