-- name: kubernetes_output__record :exec
UPDATE control_plane.kubernetes_output_stages
SET state=@state,object_version=nullif(@object_version,''),object_etag=nullif(@object_etag,''),recorded_at=clock_timestamp()
WHERE id=@id::uuid AND organization_id=@organization_id::uuid AND ref=@ref
 AND state='ATTEMPTING' AND object_version IS NULL;
