SELECT metadata.schema_version,
       interaction_gateway_runtime_identity_ready($1, $2, $3)
FROM interaction_gateway_metadata AS metadata WHERE metadata.singleton = true;
