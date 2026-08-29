-- name: bootstrap_component_insert_secondary_provider :exec
INSERT INTO control_plane.provider_accounts
    (ref, organization_id, definition_key, stable_key, name,
     state, enabled, created_by)
SELECT 'pacc_component_secondary', organization.id, 'openai-codex',
       'component-secondary', 'Component secondary account',
       'AUTHORIZED', true, subject.id
FROM control_plane.organizations organization
JOIN control_plane.subjects subject
  ON subject.organization_id = organization.id
 AND subject.ref = 'sys_platform';

INSERT INTO control_plane.provider_credential_revisions
    (ref, organization_id, provider_account_id, revision_number,
     secret_name, secret_uid, secret_resource_version, content_sha256,
     observed_at)
SELECT 'pcr_component_secondary', account.organization_id, account.id, 1,
       'runtime-provider-openai-component-secondary',
       '30000000-0000-4000-8000-000000000001'::uuid, '1',
       'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855',
       clock_timestamp()
FROM control_plane.provider_accounts account
WHERE account.stable_key = 'component-secondary';

UPDATE control_plane.provider_accounts account
SET current_credential_revision_id = credential.id,
    version = account.version + 1,
    updated_at = clock_timestamp()
FROM control_plane.provider_credential_revisions credential
WHERE account.id = credential.provider_account_id;
