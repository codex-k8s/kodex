-- name: revision_impact__finish :exec
UPDATE control_plane.revision_impact_plans SET state='APPLIED',version=version+1,
 published_revision_ref=@revision_ref,applied_at=clock_timestamp()
WHERE id=@plan_id::uuid AND state='PREPARED' AND expires_at>clock_timestamp()
 AND NOT EXISTS(SELECT 1 FROM control_plane.revision_impact_items WHERE plan_id=@plan_id::uuid AND outcome='PENDING');
