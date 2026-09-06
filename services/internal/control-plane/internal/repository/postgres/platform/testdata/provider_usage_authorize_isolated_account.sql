-- name: provider_usage_authorize_isolated_account :exec
WITH credential AS (
 INSERT INTO control_plane.provider_credential_revisions
  (ref,organization_id,provider_account_id,revision_number,secret_name,secret_uid,
   secret_resource_version,content_sha256,observed_at)
 SELECT 'pcr_usage_lock_fixture',account.organization_id,account.id,1,
  'runtime-provider-usage-lock-fixture','70000000-0000-4000-8000-000000000001'::uuid,
  '1','e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855',clock_timestamp()
 FROM control_plane.provider_accounts account
 WHERE account.ref=$1 AND account.state='PENDING_AUTHORIZATION'
 RETURNING id,provider_account_id
)
UPDATE control_plane.provider_accounts account
SET current_credential_revision_id=credential.id,state='AUTHORIZED',enabled=true,
 version=account.version+1,updated_at=clock_timestamp()
FROM credential WHERE account.id=credential.provider_account_id;
