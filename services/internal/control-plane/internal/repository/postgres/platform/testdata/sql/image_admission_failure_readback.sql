-- name: image_admission_failure_readback :one
SELECT artifact.admission_state, artifact.admission_verdict, artifact.admission_failure_code,
       artifact.admission_claim_token_sha256 IS NULL, artifact.admission_claim_expires_at IS NULL,
       artifact.admission_authority_generation, artifact.sbom_sha256, artifact.admission_revision,
       attempt.ref, attempt.number, attempt.state, attempt.fence,
       attempt.finished_at IS NOT NULL AND attempt.terminal_artifact_json = to_jsonb(artifact),
       NOT EXISTS (SELECT 1 FROM control_plane.image_admission_attempts open_attempt
                   WHERE open_attempt.artifact_id = artifact.id
                     AND open_attempt.state IN ('PENDING', 'CLAIMED'))
FROM control_plane.image_artifacts artifact
JOIN control_plane.image_admission_attempts attempt ON attempt.artifact_id = artifact.id
WHERE artifact.organization_id = $1::uuid AND artifact.ref = $2
ORDER BY attempt.number DESC LIMIT 1;
