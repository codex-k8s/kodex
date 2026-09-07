\set ON_ERROR_STOP on
INSERT INTO control_plane.organizations (id, ref, name)
VALUES ('10000000-0000-4000-8000-000000000001', 'org_bootstrap_fixture', 'Synthetic bootstrap'),
       ('10000000-0000-4000-8000-000000000002', 'org_foreign_fixture', 'Synthetic foreign');
INSERT INTO control_plane.subjects (id, organization_id, ref, issuer, external_subject_digest, display_name, kind)
VALUES ('20000000-0000-4000-8000-000000000001', '10000000-0000-4000-8000-000000000001',
        'sys_platform', 'kodex-system', repeat('a', 64), 'Synthetic platform', 'SERVICE');
INSERT INTO control_plane.owner_claim_contracts (organization_id, stable_key, state)
VALUES ('10000000-0000-4000-8000-000000000001', 'installation-owner', 'PENDING_CLAIM');
INSERT INTO control_plane.provider_definitions (stable_key, name, adapter_key)
VALUES ('openai-codex', 'Synthetic Codex', 'openai-codex') ON CONFLICT DO NOTHING;
INSERT INTO control_plane.provider_accounts (ref, organization_id, definition_key, stable_key, name, state, created_by)
VALUES ('pacc_bootstrap_fixture', '10000000-0000-4000-8000-000000000001', 'openai-codex',
        'default-openai-codex', 'Synthetic default', 'AUTHORIZED', '20000000-0000-4000-8000-000000000001'),
       ('pacc_foreign_fixture', '10000000-0000-4000-8000-000000000002', 'openai-codex',
        'default-openai-codex', 'Synthetic foreign', 'AUTHORIZED', '20000000-0000-4000-8000-000000000001');
INSERT INTO control_plane.provider_accounts (ref, organization_id, definition_key, stable_key, name, state, enabled, created_by)
VALUES ('pacc_disabled_fixture', '10000000-0000-4000-8000-000000000001', 'openai-codex',
        'disabled-fixture', 'Synthetic disabled', 'PENDING_AUTHORIZATION', false, '20000000-0000-4000-8000-000000000001');
INSERT INTO control_plane.provider_credential_revisions
    (ref, organization_id, provider_account_id, revision_number, secret_name, secret_uid,
     secret_resource_version, content_sha256, observed_at)
SELECT 'pcr_disabled_fixture', organization_id, id, 1, 'provider-bootstrap-disabled',
       '30000000-0000-4000-8000-000000000001', '1', repeat('e', 64), clock_timestamp()
FROM control_plane.provider_accounts WHERE ref = 'pacc_disabled_fixture';
UPDATE control_plane.provider_accounts SET state = 'DISABLED',
    current_credential_revision_id = (SELECT id FROM control_plane.provider_credential_revisions
                                    WHERE ref = 'pcr_disabled_fixture')
WHERE ref = 'pacc_disabled_fixture';
