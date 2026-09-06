-- name: interaction_terminal_candidates :many
SELECT connection.ref, integration_grant.id::text, integration_grant.capability_key
FROM control_plane.runs run
JOIN control_plane.integration_grants integration_grant
  ON integration_grant.organization_id = run.organization_id
 AND integration_grant.target_kind = run.target_type
 AND integration_grant.target_ref = run.target_ref
 AND integration_grant.capability_key IN ('mattermost.notifications','mattermost.result_mirror')
 AND integration_grant.enabled
JOIN control_plane.integration_connections connection
  ON connection.id = integration_grant.connection_id
 AND connection.organization_id = integration_grant.organization_id
 AND connection.definition_key = 'mattermost' AND connection.enabled
 AND connection.lifecycle_state = 'ACTIVE'
 AND connection.state IN ('CONNECTED','DEGRADED')
 AND connection.definition_version = integration_grant.definition_version
 AND connection.definition_digest = integration_grant.definition_digest
WHERE run.organization_id = @organization_id::uuid AND run.id = @root_run_id::uuid
  AND run.project_id = @project_id::uuid AND run.id = run.root_run_id
  AND run.state IN ('SUCCEEDED','FAILED','CANCELLED')
  AND NOT EXISTS (SELECT 1 FROM control_plane.interaction_deliveries previous
      WHERE previous.connection_id = connection.id AND previous.root_run_id = run.id
        AND previous.capability_key = integration_grant.capability_key)
ORDER BY connection.ref, integration_grant.id
FOR UPDATE OF connection, integration_grant;
