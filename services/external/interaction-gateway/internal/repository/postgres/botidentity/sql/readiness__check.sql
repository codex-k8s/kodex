-- name: readiness__check :one
-- params: @arg1,@arg2,@arg3
SELECT metadata.schema_version,
       interaction_gateway_runtime_identity_ready(@arg1::bigint, @arg2::uuid, @arg3::jsonb)
       AND EXISTS (
           SELECT 1
           FROM pg_catalog.pg_class AS relation
           JOIN pg_catalog.pg_namespace AS namespace ON namespace.oid = relation.relnamespace
           WHERE namespace.nspname = 'public'
             AND relation.relname = 'interaction_gateway_agent_bot_catalog_cursors'
             AND relation.relrowsecurity
             AND relation.relforcerowsecurity
       )
       AND EXISTS (
           SELECT 1
           FROM pg_catalog.pg_policies AS policy
           WHERE schemaname = 'public'
             AND tablename = 'interaction_gateway_agent_bot_catalog_cursors'
             AND policyname = 'interaction_gateway_agent_bot_cursor_scope'
             AND policy.qual = policy.with_check
             AND position('interaction_gateway_runtime_scope()' IN policy.qual) > 0
             AND position('organization_id' IN policy.qual) > 0
             AND position('project_id' IN policy.qual) > 0
       )
FROM interaction_gateway_agent_bot_metadata AS metadata
WHERE metadata.singleton;
