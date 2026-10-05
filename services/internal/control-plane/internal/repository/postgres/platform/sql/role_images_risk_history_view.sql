-- name: role_images_risk_history_view :one
SELECT CASE WHEN attempt.id IS NULL THEN 'null'::jsonb ELSE jsonb_build_object(
 'Ref',attempt.ref,'ArtifactRef',artifact.ref,'RiskDecisionRef',COALESCE(decision.ref,''),
 'State',attempt.state,'Version',attempt.version,'Number',attempt.number,'Fence',attempt.fence,
 'SourceAdmissionRevision',attempt.source_admission_revision,'CreatedAt',attempt.created_at,
 'AdmissionReceiptSHA256',COALESCE(attempt.terminal_artifact_json->>'admission_receipt_sha256',''),
 'EvidenceManifestDigest',COALESCE(attempt.terminal_artifact_json->>'admission_receipt_oci_manifest_digest','')) END,
 COALESCE(decision.decision_json,'')
FROM control_plane.image_artifacts artifact
LEFT JOIN LATERAL (SELECT * FROM control_plane.image_admission_attempts
 WHERE artifact_id=artifact.id AND organization_id=artifact.organization_id ORDER BY number DESC LIMIT 1) attempt ON true
LEFT JOIN LATERAL (SELECT * FROM control_plane.image_admission_risk_decisions
 WHERE artifact_id=artifact.id AND organization_id=artifact.organization_id ORDER BY admission_revision DESC LIMIT 1) decision ON true
WHERE artifact.organization_id=@organization_id::uuid AND artifact.ref=@artifact_ref
