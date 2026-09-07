\set ON_ERROR_STOP on
BEGIN READ ONLY;
SET LOCAL statement_timeout = '10s';
SELECT (to_regclass('control_plane.owner_claim_contracts') IS NOT NULL
        AND to_regclass('control_plane.organizations') IS NOT NULL
        AND to_regclass('control_plane.provider_accounts') IS NOT NULL) AS schema_ready,
       (to_regclass('control_plane.owner_claim_contracts') IS NULL
        AND to_regclass('control_plane.organizations') IS NULL
        AND to_regclass('control_plane.provider_accounts') IS NULL) AS schema_empty
\gset
\if :schema_ready
SELECT json_build_object('owners', (SELECT count(*) FROM control_plane.owner_claim_contracts),
                         'organizations', (SELECT count(*) FROM control_plane.organizations),
                         'accounts', (SELECT count(*) FROM control_plane.provider_accounts));
\elif :schema_empty
SELECT json_build_object('owners', 0, 'organizations', 0, 'accounts', 0);
\else
SELECT json_build_object('unknown', true);
\endif
COMMIT;
