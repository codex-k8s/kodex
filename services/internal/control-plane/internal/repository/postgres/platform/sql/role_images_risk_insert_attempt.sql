-- name: role_images_risk_insert_attempt :one
INSERT INTO control_plane.image_admission_attempts
(ref,organization_id,artifact_id,number,risk_decision_id,source_admission_revision,source_receipt_sha256,source_evidence_manifest_digest,source_artifact_json,state)
SELECT @ref,@organization_id::uuid,@artifact_id::uuid,COALESCE(MAX(number),0)+1,NULLIF(@risk_decision_id,'')::uuid,@source_admission_revision,@source_receipt_sha256,@source_evidence_manifest_digest,@source_artifact_json,@state
FROM control_plane.image_admission_attempts WHERE artifact_id=@artifact_id::uuid
RETURNING number,created_at
