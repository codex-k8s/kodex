-- name: role_images_risk_get_attempt :one
SELECT attempt.ref,attempt.number,attempt.state,attempt.version,attempt.fence,
 COALESCE(decision.ref,''),COALESCE(decision.decision_json,''),COALESCE(decision.risk_acceptance_json,''),
 COALESCE(decision.risk_acceptance_sha256,''),attempt.source_admission_revision,
 attempt.source_receipt_sha256,attempt.source_evidence_manifest_digest,attempt.created_at
FROM control_plane.image_admission_attempts attempt
LEFT JOIN control_plane.image_admission_risk_decisions decision ON decision.id=attempt.risk_decision_id
WHERE attempt.organization_id=@organization_id::uuid AND attempt.artifact_id=@artifact_id::uuid
ORDER BY attempt.number DESC LIMIT 1
FOR UPDATE OF attempt
