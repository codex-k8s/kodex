-- name: kubernetes_input__unknown :exec
UPDATE control_plane.kubernetes_input_stages SET state='UNKNOWN'
WHERE id=@stage_id::uuid AND organization_id=@organization_id::uuid AND state='CLAIMED';
