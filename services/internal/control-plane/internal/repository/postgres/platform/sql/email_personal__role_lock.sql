-- name: email_personal__role_lock :exec
SELECT pg_catalog.pg_advisory_xact_lock(pg_catalog.hashtextextended('personal-email-metadata-role:'||@organization_id::text,0));
