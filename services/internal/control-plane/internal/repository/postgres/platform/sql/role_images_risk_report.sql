-- name: role_images_risk_report :one
SELECT projection_json,projection_sha256,artifact_version,admission_receipt_sha256,evidence_manifest_digest
FROM control_plane.image_vulnerability_reports
WHERE organization_id=@organization_id::uuid AND artifact_id=@artifact_id::uuid
 AND admission_revision=@admission_revision
