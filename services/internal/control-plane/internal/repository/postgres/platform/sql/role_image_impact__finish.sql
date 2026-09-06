-- name: role_image_impact__finish :one
UPDATE control_plane.role_image_impact_plans
SET state='APPLIED',version=version+1,applied_at=clock_timestamp()
WHERE id=@plan_id::uuid AND state='PREPARED' AND expires_at>clock_timestamp()
 AND NOT EXISTS(SELECT 1 FROM control_plane.role_image_impact_items WHERE plan_id=@plan_id::uuid AND outcome='PENDING')
RETURNING version;
