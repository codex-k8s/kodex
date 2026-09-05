-- name: configuration__accept :one
INSERT INTO email_bridge.configuration_watermark (singleton, revision, digest)
VALUES (true, @revision, @digest)
ON CONFLICT (singleton) DO UPDATE SET revision=EXCLUDED.revision,digest=EXCLUDED.digest
WHERE email_bridge.configuration_watermark.revision < EXCLUDED.revision
OR (email_bridge.configuration_watermark.revision = EXCLUDED.revision AND email_bridge.configuration_watermark.digest=EXCLUDED.digest)
RETURNING revision;
