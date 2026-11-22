-- name: kubernetes_output__admit :exec
UPDATE control_plane.kubernetes_output_stages
SET state='ACCEPTED',delivery_ordinal=@delivery_ordinal,authorized_until=@authorized_until,
 accepted_at=coalesce(accepted_at,clock_timestamp())
WHERE id=@id::uuid AND organization_id=@organization_id::uuid
 AND state IN('STAGED','ACCEPTED') AND object_version IS NOT NULL;
