-- name: integration_health_refresh__create :exec
INSERT INTO control_plane.integration_connection_tests
    (ref,organization_id,connection_id,state,created_by,purpose,attempt,predecessor_ref)
VALUES (@ref,@organization_id::uuid,@connection_id::uuid,'DUE',@actor_id::uuid,
        'MANAGED_MCP_REFRESH',@attempt,NULLIF(@predecessor_ref,''));
