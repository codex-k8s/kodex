\set ON_ERROR_STOP on
BEGIN;
SET LOCAL statement_timeout = '10s';
-- Только новая explicit import identity. Existing account не включается повторно.
WITH created AS (
INSERT INTO control_plane.provider_accounts
    (ref, organization_id, definition_key, stable_key, name, state, enabled,
     max_concurrent_executions, created_by)
SELECT :'account_ref', owner.organization_id, 'openai-codex', :'stable_key', :'account_name',
       'PENDING_AUTHORIZATION', true, :'max_concurrent_executions'::integer, subject.id
FROM control_plane.owner_claim_contracts owner
JOIN control_plane.subjects subject ON subject.organization_id = owner.organization_id
  AND subject.ref = 'sys_platform' AND subject.issuer = 'kodex-system'
  AND subject.kind = 'SERVICE' AND subject.active
WHERE owner.stable_key = 'installation-owner' AND :'stable_key' <> 'default-openai-codex'
ON CONFLICT (ref) DO NOTHING
RETURNING ref, organization_id, created_by)
INSERT INTO control_plane.audit_events
    (ref, organization_id, actor_id, action, resource_kind, resource_ref, outcome, safe_summary, correlation_ref)
SELECT 'audit_' || ref, organization_id, created_by, 'provider_account.bootstrap_reserved',
       'PROVIDER_ACCOUNT', ref, 'SUCCEEDED', 'i18n:PROVIDER_ACCOUNT_CREATED', ref
FROM created;
COMMIT;
