-- name: commands_changeworkflow_validate_agent_capabilities :one
SELECT EXISTS (
    SELECT 1
    FROM control_plane.agents a
    WHERE a.organization_id=$1::uuid
      AND a.project_id=$2::uuid
      AND a.ref=$3
      AND a.enabled
      AND a.state='READY'
      AND NOT EXISTS (
          SELECT 1
          FROM unnest($4::text[]) required(capability_key)
          WHERE NOT (
              required.capability_key=ANY(a.capabilities)
              OR EXISTS (
                  SELECT 1
                  FROM control_plane.integration_grants g
                  JOIN control_plane.integration_connections c ON c.id=g.connection_id
                  WHERE g.organization_id=a.organization_id
                    AND g.target_kind='AGENT'
                    AND g.target_ref=a.ref
                    AND g.capability_key=required.capability_key
                    AND g.enabled
                    AND c.enabled
                    AND c.state='CONNECTED'
              )
          )
      )
)
