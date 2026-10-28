-- name: ssh_compose__source_grants :many
SELECT g.ref,g.version,g.capability_key
FROM control_plane.integration_grants g
JOIN control_plane.integration_connections c ON c.id=g.connection_id
JOIN control_plane.run_nodes n ON n.id=@node_id::uuid
JOIN control_plane.agents a ON a.id=n.agent_id
WHERE g.organization_id=@organization_id::uuid AND c.ref=@connection_ref
  AND c.enabled AND c.state='CONNECTED' AND c.definition_key='github'
  AND g.enabled AND g.target_kind='AGENT' AND g.target_ref=a.ref
  AND g.risk='READ' AND g.approval_policy='NONE'
  AND g.capability_key IN ('github.commit.read','github.repository.content.list','github.repository.content.read')
  AND g.definition_version=c.definition_version AND g.definition_digest=c.definition_digest
  AND g.resource_scope->>'owner'=@owner AND g.resource_scope->>'repository'=@repository
  AND EXISTS (
    SELECT 1 FROM control_plane.runtime_revisions revision,
      jsonb_array_elements(COALESCE(revision.safe_snapshot->'integrationGrants','[]'::jsonb)) binding
    WHERE revision.organization_id=g.organization_id AND revision.node_id=n.id
      AND revision.id=@runtime_revision_id::uuid AND revision.run_id=n.run_id AND revision.root_run_id=n.root_run_id
      AND revision.agent_id=n.agent_id AND revision.turn_id=n.turn_id AND revision.attempt=n.attempt
      AND binding->>'ref'=g.ref AND binding->>'grantVersion'=g.version::text
      AND binding->>'capabilityKey'=g.capability_key AND binding->>'approvalPolicy'='NONE'
  )
ORDER BY g.capability_key
FOR SHARE OF g,c NOWAIT;
