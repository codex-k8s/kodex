-- name: secret_draft_operation_finish :exec
UPDATE control_plane.runtime_secret_draft_operations SET state=@state,failure_code=@failure_code,
terminal_snapshot=@snapshot::jsonb,updated_at=clock_timestamp()
WHERE id=@operation_id::uuid;
