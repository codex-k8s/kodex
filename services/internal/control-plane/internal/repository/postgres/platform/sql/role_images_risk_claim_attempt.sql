-- name: role_images_risk_claim_attempt :execrows
UPDATE control_plane.image_admission_attempts SET state='CLAIMED',version=version+1,fence=@fence
WHERE organization_id=@organization_id::uuid AND artifact_id=@artifact_id::uuid
 AND ref=@ref AND number=@number AND state='PENDING'
