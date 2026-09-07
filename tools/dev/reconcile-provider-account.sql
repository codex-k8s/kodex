\set ON_ERROR_STOP on
BEGIN;
SET LOCAL statement_timeout = '10s';
SET LOCAL lock_timeout = '5s';
SELECT account.id AS account_id, account.organization_id AS organization_id, actor.id AS actor_id,
       account.version AS actual_version, COALESCE(revision.ref, '') AS actual_credential_ref,
       COALESCE(revision.secret_name = :'secret_name' AND revision.secret_uid = :'secret_uid'::uuid
         AND revision.secret_resource_version = :'secret_resource_version'
         AND revision.content_sha256 = :'content_sha256', false) AS replay
FROM control_plane.owner_claim_contracts installation_owner
JOIN control_plane.provider_accounts account ON account.organization_id = installation_owner.organization_id
JOIN control_plane.subjects actor ON actor.organization_id = account.organization_id
  AND actor.ref = 'sys_platform' AND actor.issuer = 'kodex-system' AND actor.kind = 'SERVICE' AND actor.active
LEFT JOIN control_plane.provider_credential_revisions revision ON revision.id = account.current_credential_revision_id
WHERE installation_owner.stable_key = 'installation-owner'
  AND account.ref = :'account_ref' AND account.stable_key = :'stable_key'
  AND account.definition_key = 'openai-codex' AND account.enabled
  AND account.state IN ('AUTHORIZED', 'REAUTHORIZATION_REQUIRED', 'PENDING_AUTHORIZATION')
FOR UPDATE OF account
\gset
-- Exact повтор не меняет состояние или version; другой descriptor требует оба pins.
SELECT 1 / CASE WHEN :'replay'::boolean OR
  (:'actual_version'::bigint = :'expected_version'::bigint AND
   :'actual_credential_ref' = :'expected_credential_ref') THEN 1 ELSE 0 END AS cas_guard
\gset
\if :replay
\else
INSERT INTO control_plane.provider_credential_revisions
    (ref, organization_id, provider_account_id, revision_number, secret_name, secret_uid,
     secret_resource_version, content_sha256, observed_at)
SELECT :'credential_ref', :'organization_id'::uuid, :'account_id'::uuid,
       COALESCE(max(revision_number), 0) + 1, :'secret_name', :'secret_uid'::uuid,
       :'secret_resource_version', :'content_sha256', clock_timestamp()
FROM control_plane.provider_credential_revisions WHERE provider_account_id = :'account_id'::uuid
RETURNING id AS next_revision_id
\gset
UPDATE control_plane.provider_accounts SET current_credential_revision_id = :'next_revision_id'::uuid,
    state = 'AUTHORIZED', version = version + 1, updated_at = clock_timestamp()
WHERE id = :'account_id'::uuid;
INSERT INTO control_plane.audit_events
    (ref, organization_id, actor_id, action, resource_kind, resource_ref, outcome, safe_summary, correlation_ref)
VALUES ('audit_' || :'credential_ref', :'organization_id'::uuid, :'actor_id'::uuid,
        'provider_account.bootstrap_credential_imported', 'PROVIDER_ACCOUNT', :'account_ref',
        'SUCCEEDED', 'i18n:PROVIDER_ACCOUNT_AUTHORIZED', :'credential_ref');
\endif
COMMIT;
SELECT to_json(true);
