-- name: team_readiness__check :one
-- params: @arg1,@arg2,@arg3
SELECT metadata.schema_version,
       interaction_gateway_runtime_identity_ready(@arg1::bigint, @arg2::uuid, @arg3::jsonb)
FROM interaction_gateway_team_metadata AS metadata
WHERE metadata.singleton;
