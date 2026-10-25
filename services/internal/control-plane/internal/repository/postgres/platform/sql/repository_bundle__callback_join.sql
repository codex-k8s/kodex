-- name: repository_bundle__callback_join :one
SELECT publication.origin_node_id::text
FROM control_plane.repository_bundle_publications publication
JOIN control_plane.integration_invocations invocation
  ON invocation.id=publication.integration_invocation_id AND invocation.organization_id=publication.organization_id
 AND invocation.operation='github.repository.bundle.publish' AND invocation.node_id=publication.origin_node_id
 AND invocation.run_id=publication.origin_run_id
WHERE publication.organization_id=@organization_id::uuid AND invocation.id=@invocation_id::uuid
  AND EXISTS (SELECT 1 FROM control_plane.run_edges callback
    WHERE callback.organization_id=publication.organization_id AND callback.root_run_id=publication.root_run_id
      AND callback.target_node_id=publication.origin_node_id AND callback.type='CALLBACK_TO');
