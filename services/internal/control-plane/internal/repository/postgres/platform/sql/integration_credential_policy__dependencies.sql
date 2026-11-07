-- name: integration_credential_policy__dependencies :one
SELECT connection.credential_policy,
       (SELECT count(DISTINCT root.id)
        FROM control_plane.runtime_revisions revision
        JOIN control_plane.runs root ON root.id=revision.root_run_id
             AND root.organization_id=revision.organization_id
        WHERE revision.organization_id=connection.organization_id
          AND root.state IN ('QUEUED','RUNNING','WAITING_HUMAN','CANCELLING')
          AND revision.safe_snapshot->'integrationGrants' @>
              jsonb_build_array(jsonb_build_object('connectionRef',connection.ref))) AS active_roots
FROM control_plane.integration_connections connection
WHERE connection.organization_id=@organization_id::uuid
  AND connection.id=@connection_id::uuid
  AND connection.lifecycle_state='ACTIVE'
