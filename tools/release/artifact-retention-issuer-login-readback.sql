BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout='5s';
SELECT jsonb_build_object(
 'configured', rolpassword IS NOT NULL,
 'scram', COALESCE(rolpassword LIKE 'SCRAM-SHA-256$%',false),
 'verifierSHA256', encode(sha256(convert_to(rolpassword,'UTF8')),'hex'))
FROM pg_catalog.pg_authid
WHERE rolname='ira_artifact_retention_issuer_g1';
ROLLBACK;
