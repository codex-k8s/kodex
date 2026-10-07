-- name: role_images_admission_claim_replay_live :one
SELECT EXISTS (
 SELECT 1 FROM control_plane.image_artifacts artifact
 WHERE artifact.organization_id = @organization_id::uuid AND artifact.ref = @artifact_ref
 AND artifact.admission_state = 'CLAIMED' AND artifact.version = @version
 AND artifact.admission_fence = @fence AND artifact.admission_authority_generation = @generation
 AND artifact.admission_claimant_workload = 'image-admission'
 AND artifact.admission_claim_token_sha256 = @token_sha256
 AND artifact.admission_claim_expires_at > clock_timestamp()
)
