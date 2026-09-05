-- name: secret_draft_impact_finish :exec
UPDATE control_plane.runtime_secret_draft_impact_plans SET state=@state WHERE operation_id=@operation_id::uuid AND state='PREPARED';
