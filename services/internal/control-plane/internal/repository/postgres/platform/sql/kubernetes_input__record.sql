-- name: kubernetes_input__record :exec
UPDATE control_plane.kubernetes_input_stages SET object_version=nullif(@object_version,''),object_etag=nullif(@object_etag,''),
 state=CASE WHEN state='ATTEMPTING' THEN @state ELSE state END,recorded_at=clock_timestamp()
WHERE id=@stage_id::uuid AND organization_id=@organization_id::uuid AND recorded_at IS NULL;
