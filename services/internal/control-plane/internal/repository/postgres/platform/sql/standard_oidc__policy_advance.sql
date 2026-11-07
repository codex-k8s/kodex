-- name: standard_oidc__policy_advance :exec
INSERT INTO control_plane.oidc_identity_policy_current(singleton, revision)
VALUES (true, @revision)
ON CONFLICT (singleton) DO UPDATE SET revision = EXCLUDED.revision
WHERE oidc_identity_policy_current.revision < EXCLUDED.revision;
