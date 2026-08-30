\set ON_ERROR_STOP on

BEGIN;

INSERT INTO control_plane.provider_accounts
    (ref, organization_id, definition_key, stable_key, name,
     external_account_masked, state, enabled, created_by)
SELECT :'account_ref', organization.id, 'openai-codex', :'stable_key',
       :'account_name', '', 'AUTHORIZED', true, subject.id
FROM control_plane.organizations organization
JOIN control_plane.subjects subject
  ON subject.organization_id = organization.id
 AND subject.ref = 'sys_platform'
ORDER BY organization.created_at
LIMIT 1
ON CONFLICT (ref) DO UPDATE
SET name = EXCLUDED.name,
    state = 'AUTHORIZED',
    enabled = true,
    version = control_plane.provider_accounts.version + 1,
    updated_at = clock_timestamp()
WHERE control_plane.provider_accounts.organization_id = EXCLUDED.organization_id
  AND control_plane.provider_accounts.definition_key = EXCLUDED.definition_key
  AND control_plane.provider_accounts.stable_key = EXCLUDED.stable_key
  AND (control_plane.provider_accounts.name IS DISTINCT FROM EXCLUDED.name
    OR control_plane.provider_accounts.state IS DISTINCT FROM 'AUTHORIZED'
    OR control_plane.provider_accounts.enabled IS DISTINCT FROM true);

INSERT INTO control_plane.provider_credential_revisions
    (ref, organization_id, provider_account_id, revision_number,
     secret_name, secret_uid, secret_resource_version, content_sha256,
     observed_at)
SELECT :'credential_ref', account.organization_id, account.id,
       COALESCE((
           SELECT max(revision.revision_number)
           FROM control_plane.provider_credential_revisions revision
           WHERE revision.provider_account_id = account.id
       ), 0) + 1,
       :'secret_name', :'secret_uid'::uuid, :'secret_resource_version',
       :'content_sha256', clock_timestamp()
FROM control_plane.provider_accounts account
WHERE account.ref = :'account_ref'
  AND account.definition_key = 'openai-codex'
  AND account.stable_key = :'stable_key'
  AND account.organization_id = (
      SELECT organization.id
      FROM control_plane.organizations organization
      ORDER BY organization.created_at
      LIMIT 1
  )
  AND NOT EXISTS (
      SELECT 1
      FROM control_plane.provider_credential_revisions revision
      WHERE revision.provider_account_id = account.id
        AND revision.secret_uid = :'secret_uid'::uuid
        AND revision.secret_resource_version = :'secret_resource_version'
        AND revision.secret_name = :'secret_name'
        AND revision.content_sha256 = :'content_sha256'
  );

UPDATE control_plane.provider_accounts account
SET current_credential_revision_id = revision.id,
    state = 'AUTHORIZED',
    enabled = true,
    version = account.version + 1,
    updated_at = clock_timestamp()
FROM control_plane.provider_credential_revisions revision
WHERE account.ref = :'account_ref'
  AND account.definition_key = 'openai-codex'
  AND account.stable_key = :'stable_key'
  AND account.organization_id = (
      SELECT organization.id
      FROM control_plane.organizations organization
      ORDER BY organization.created_at
      LIMIT 1
  )
  AND revision.provider_account_id = account.id
  AND revision.secret_uid = :'secret_uid'::uuid
  AND revision.secret_resource_version = :'secret_resource_version'
  AND revision.secret_name = :'secret_name'
  AND revision.content_sha256 = :'content_sha256'
  AND account.current_credential_revision_id IS DISTINCT FROM revision.id;

SELECT account.stable_key,
       revision.revision_number,
       revision.secret_name
FROM control_plane.provider_accounts account
JOIN control_plane.provider_credential_revisions revision
  ON revision.id = account.current_credential_revision_id
WHERE account.ref = :'account_ref'
  AND account.definition_key = 'openai-codex'
  AND account.stable_key = :'stable_key'
  AND account.organization_id = (
      SELECT organization.id
      FROM control_plane.organizations organization
      ORDER BY organization.created_at
      LIMIT 1
  );

COMMIT;
