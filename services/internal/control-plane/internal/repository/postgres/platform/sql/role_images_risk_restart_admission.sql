-- name: role_images_risk_restart_admission :one
UPDATE control_plane.image_artifacts SET admission_state='PENDING',admission_verdict='',
 admission_claimant_workload=NULL,admission_authority_generation=0,admission_fence=admission_fence+1,
 admission_claim_token_sha256=NULL,admission_claim_expires_at=NULL,
 promotion_state='REJECTED',promotion_claimant_workload=NULL,promotion_authority_generation=0,
 promotion_fence=promotion_fence+1,promotion_claim_token_sha256=NULL,promotion_claim_expires_at=NULL,
 promotion_authorization_token_sha256=NULL,promotion_authorization_expires_at=NULL,promotion_request_id=NULL,
 version=version+1,updated_at=clock_timestamp()
WHERE organization_id=@organization_id::uuid AND id=@artifact_id::uuid
 AND version=@version AND admission_state='REJECTED'
RETURNING version,updated_at
