-- name: interaction_enqueue_terminal_deliveries :exec
INSERT INTO control_plane.interaction_deliveries (
    ref,
    organization_id,
    project_id,
    connection_id,
    grant_id,
    root_run_id,
    capability_key,
    message_key,
    template_data,
    state
)
SELECT
    'idl_' || replace(gen_random_uuid()::text, '-', ''),
    @organization_id::uuid,
    @project_id::uuid,
    integration_grant.connection_id,
    integration_grant.id,
    run.id,
    integration_grant.capability_key,
    CASE integration_grant.capability_key
      WHEN 'mattermost.notifications' THEN 'MATTERMOST_RUN_NOTIFICATION'
      ELSE 'MATTERMOST_RUN_RESULT'
    END,
    jsonb_build_object(
        'title', left(run.title, 300),
        'state', run.state,
        'result', left(run.result_summary, 4000),
        'runRef', run.ref,
        'artifactCount', (
            SELECT count(*)
            FROM control_plane.artifacts artifact
            JOIN control_plane.runs artifact_run ON artifact_run.id = artifact.run_id
            WHERE artifact_run.root_run_id = run.id
               OR artifact_run.id = run.id
        )
    ),
    'DUE'
FROM control_plane.runs run
JOIN control_plane.integration_grants integration_grant
  ON integration_grant.organization_id = @organization_id::uuid
 AND integration_grant.target_kind = run.target_type
 AND integration_grant.target_ref = run.target_ref
 AND integration_grant.capability_key IN ('mattermost.notifications', 'mattermost.result_mirror')
 AND integration_grant.enabled
JOIN control_plane.integration_connections connection
  ON connection.id = integration_grant.connection_id
 AND connection.definition_key = 'mattermost'
 AND connection.enabled
 AND connection.state IN ('CONNECTED', 'DEGRADED')
WHERE run.id = @root_run_id::uuid
  AND run.id = run.root_run_id
  AND run.state IN ('SUCCEEDED', 'FAILED', 'CANCELLED')
ON CONFLICT DO NOTHING
