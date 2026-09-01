-- name: artifacts_downloadartifact_insert_download_grant :one
INSERT INTO control_plane.artifact_download_grants(
    ref,
    organization_id,
    project_id,
    artifact_id,
    artifact_version,
    subject_id,
    purpose,
    expires_at
)
VALUES (
    @grant_ref,
    @organization_id::uuid,
    NULLIF(@project_id, '')::uuid,
    @artifact_id::uuid,
    @artifact_version,
    @subject_id::uuid,
    @purpose,
    clock_timestamp() + interval '1 minute'
)
RETURNING id, ref;
