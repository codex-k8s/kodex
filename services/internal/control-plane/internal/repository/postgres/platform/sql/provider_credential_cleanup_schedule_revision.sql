-- name: provider_credential_cleanup_schedule_revision :exec
INSERT INTO control_plane.provider_credential_cleanup_tasks (
    ref, organization_id, provider_account_id, provider_credential_revision_id,
    secret_name, secret_uid, secret_resource_version, content_sha256, eligible_at,
    maximum_attempts
)
SELECT 'pcct_' || gen_random_uuid()::text,
       revision.organization_id,
       revision.provider_account_id,
       revision.id,
       revision.secret_name,
       revision.secret_uid,
       revision.secret_resource_version,
       revision.content_sha256,
       @eligible_at,
       @maximum_attempts
FROM control_plane.provider_credential_revisions revision
WHERE revision.id = @credential_revision_id::uuid
  AND revision.organization_id = @organization_id::uuid
  AND revision.provider_account_id = @account_id::uuid
ON CONFLICT (provider_credential_revision_id) DO UPDATE
SET eligible_at = LEAST(
        control_plane.provider_credential_cleanup_tasks.eligible_at,
        EXCLUDED.eligible_at
    ),
    updated_at = clock_timestamp()
WHERE control_plane.provider_credential_cleanup_tasks.state = 'PENDING';
