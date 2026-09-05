-- name: secret_draft_impact_cancel :exec
UPDATE control_plane.runtime_secret_draft_impact_plans SET state='CANCELLED' WHERE draft_id=@draft_id::uuid AND state='PREPARED';
