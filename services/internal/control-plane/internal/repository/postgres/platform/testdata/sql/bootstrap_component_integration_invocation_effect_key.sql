-- name: BootstrapComponentIntegrationInvocationEffectKey :one
SELECT effect_key
FROM control_plane.integration_invocations
WHERE ref = $1;
