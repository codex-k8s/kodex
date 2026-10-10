-- name: prepared_content_abandon :exec
UPDATE control_plane.prepared_content SET state='ABANDONED',generation=generation+1,
 available_until=clock_timestamp(),updated_at=clock_timestamp()
WHERE id=@id AND organization_id=@organization_id AND state IN ('STAGED','UNKNOWN')
 AND adopted_revision_id IS NULL;
