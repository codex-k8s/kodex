SELECT version, maximum_age_seconds, activation_id::text FROM internal_rpc_authority.authority_freshness_policy WHERE singleton;
