-- name: kubernetes_execution__effect_complete :exec
UPDATE control_plane.kubernetes_execution_effects SET state=@state,closed_at=coalesce(closed_at,clock_timestamp())
WHERE organization_id=@organization_id::uuid AND id=@effect_id::uuid AND state IN('ATTEMPTING','UNKNOWN','CANCELLED');
