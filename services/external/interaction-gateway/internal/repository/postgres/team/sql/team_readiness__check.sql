SELECT metadata.schema_version,
       interaction_gateway_runtime_identity_ready($1::bigint, $2::uuid, $3::jsonb)
FROM interaction_gateway_team_metadata AS metadata
WHERE metadata.singleton;
