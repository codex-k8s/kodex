-- name: RuntimeCredentialState
SELECT fence.current_high_watermark,
       coalesce((
           SELECT principal.status
             FROM integration_gateway.runtime_principals AS principal
            WHERE principal.generation = @current_generation
       ), '') AS requested_current_status
  FROM integration_gateway.runtime_credential_fence AS fence
 WHERE fence.singleton
