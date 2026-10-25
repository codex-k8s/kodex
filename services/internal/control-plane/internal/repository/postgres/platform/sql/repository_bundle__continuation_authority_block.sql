-- name: repository_bundle__continuation_authority_block :one
SELECT node.ref,node.run_id::text
FROM control_plane.repository_bundle_publications publication
JOIN control_plane.run_nodes node ON node.id=@node_id::uuid AND node.organization_id=publication.organization_id
 AND node.root_run_id=publication.root_run_id AND node.state='SUCCEEDED'
JOIN control_plane.runs root ON root.id=node.root_run_id AND root.organization_id=node.organization_id AND root.state IN ('RUNNING','WAITING_HUMAN')
JOIN control_plane.runs run ON run.id=node.run_id AND run.organization_id=node.organization_id AND run.project_id=publication.project_id
JOIN control_plane.agents agent ON agent.id=node.agent_id AND agent.organization_id=node.organization_id
WHERE publication.organization_id=@organization_id::uuid AND publication.ref=@proposal_ref
 AND publication.state IN ('SUCCEEDED','REJECTED','CANCELLED','EXPIRED','FAILED') AND publication.continuation_state=''
 AND NOT EXISTS (SELECT 1 FROM control_plane.run_edges continuation WHERE continuation.organization_id=node.organization_id
   AND continuation.root_run_id=node.root_run_id AND continuation.source_node_id=node.id AND continuation.type='CONTINUES')
 AND (run.id<>publication.origin_run_id OR NOT EXISTS (SELECT 1 FROM control_plane.callback_receipts delivered
   WHERE delivered.child_run_id=run.id))
 AND NOT (agent.enabled AND agent.state='READY'
   AND EXISTS (SELECT 1 FROM control_plane.projects project WHERE project.id=run.project_id AND project.lifecycle='ACTIVE')
   AND EXISTS (SELECT 1 FROM control_plane.subjects actor
     JOIN control_plane.catalog_access_targets target ON target.organization_id=actor.organization_id AND target.kind='AGENT' AND target.id=agent.id
     WHERE actor.id=root.initiated_by AND actor.organization_id=node.organization_id AND actor.active
       AND control_plane.catalog_resource_visible(node.organization_id,actor.id,'agent.launch','AGENT',target.id,target.project_id,target.owner_id,target.related_ids,statement_timestamp(),false)))
 AND (publication.origin_node_id=node.id OR EXISTS (SELECT 1 FROM control_plane.run_edges callback
   JOIN control_plane.callback_receipts receipt ON receipt.callback_edge_id=callback.id
     AND receipt.child_run_id=publication.origin_run_id
   JOIN control_plane.runs receipt_child ON receipt_child.id=receipt.child_run_id
     AND receipt_child.organization_id=node.organization_id AND receipt_child.root_run_id=node.root_run_id
     AND receipt_child.project_id=publication.project_id
   WHERE callback.organization_id=node.organization_id AND callback.root_run_id=node.root_run_id AND callback.target_node_id=node.id AND callback.type='CALLBACK_TO'));
