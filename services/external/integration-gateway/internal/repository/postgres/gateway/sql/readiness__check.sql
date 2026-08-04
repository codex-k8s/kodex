-- name: ReadinessCheck
SELECT pg_has_role(session_user, 'integration_gateway_runtime', 'member'),
       integration_gateway.runtime_identity_ready(),
       to_regclass('integration_gateway.continuation_effects') IS NOT NULL
       AND to_regprocedure('integration_gateway.next_continuation_scope()') IS NOT NULL
       AND to_regprocedure('integration_gateway.result_reference_digest(text,text,text)') IS NOT NULL
       AND integration_gateway.continuation_readiness()
