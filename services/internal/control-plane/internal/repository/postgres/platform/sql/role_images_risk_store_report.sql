-- name: role_images_risk_store_report :exec
INSERT INTO control_plane.image_vulnerability_reports
(artifact_id,admission_revision,organization_id,projection_json,projection_sha256,artifact_version,admission_receipt_sha256,evidence_manifest_digest)
VALUES (@artifact_id::uuid,@admission_revision,@organization_id::uuid,@projection_json,@projection_sha256,@artifact_version,@receipt_sha256,@evidence_manifest_digest)
