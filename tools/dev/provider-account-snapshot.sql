\set ON_ERROR_STOP on
BEGIN READ ONLY;
SET LOCAL statement_timeout = '10s';
SELECT COALESCE(json_agg(json_build_object(
    'accountRef', account.ref, 'version', account.version, 'state', account.state,
    'enabled', account.enabled, 'credentialRef', COALESCE(revision.ref, ''),
    'credential', CASE WHEN revision.id IS NULL THEN NULL ELSE json_build_object(
        'secretName', revision.secret_name, 'secretUID', revision.secret_uid::text,
        'secretResourceVersion', revision.secret_resource_version,
        'contentSHA256', revision.content_sha256) END)), '[]'::json)
FROM control_plane.owner_claim_contracts installation_owner
JOIN control_plane.provider_accounts account ON account.organization_id = installation_owner.organization_id
LEFT JOIN control_plane.provider_credential_revisions revision ON revision.id = account.current_credential_revision_id
WHERE installation_owner.stable_key = 'installation-owner'
  AND account.definition_key = 'openai-codex' AND account.stable_key = :'stable_key';
COMMIT;
