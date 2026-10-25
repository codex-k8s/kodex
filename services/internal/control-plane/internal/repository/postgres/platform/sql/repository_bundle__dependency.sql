-- name: repository_bundle__dependency :many
SELECT publication.ref
FROM control_plane.repository_bundle_publications publication
WHERE publication.organization_id=@organization_id::uuid
  AND publication.state IN ('WAITING_APPROVAL','QUEUED','CLAIMED','EFFECT_STARTED','UNKNOWN_OUTCOME')
  AND (
    (@source_kind='CONNECTION' AND EXISTS (SELECT 1 FROM control_plane.integration_connections connection WHERE connection.id=publication.connection_id AND connection.ref=@source_ref))
    OR (@source_kind='AGENT' AND EXISTS (SELECT 1 FROM control_plane.run_nodes node JOIN control_plane.agents agent ON agent.id=node.agent_id WHERE node.id=publication.origin_node_id AND agent.ref=@source_ref))
    OR (@source_kind='ARTIFACT' AND EXISTS (SELECT 1 FROM control_plane.artifact_revisions revision JOIN control_plane.artifact_heads head ON head.id=revision.artifact_id WHERE revision.ref IN (publication.archive_revision_ref,publication.manifest_revision_ref) AND head.ref=@source_ref))
  )
ORDER BY publication.root_run_id,publication.integration_invocation_id,publication.id
LIMIT 257;
