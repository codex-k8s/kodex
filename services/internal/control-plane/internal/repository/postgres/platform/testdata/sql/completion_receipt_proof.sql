-- name: completion_receipt_proof :one
WITH selected AS (SELECT id,organization_id,project_id,state FROM control_plane.runs WHERE ref=@run_ref)
SELECT root.state,lease.state,
 (SELECT count(*) FROM control_plane.artifact_heads artifact WHERE artifact.run_id=root.id),
 (SELECT count(*) FROM control_plane.artifact_revisions revision JOIN control_plane.artifact_heads artifact ON artifact.id=revision.artifact_id WHERE artifact.run_id=root.id),
 (SELECT count(*) FROM control_plane.artifact_revision_content content JOIN control_plane.artifact_revisions revision ON revision.id=content.revision_id JOIN control_plane.artifact_heads artifact ON artifact.id=revision.artifact_id WHERE artifact.run_id=root.id AND content.digest=@artifact_digest),
 (SELECT count(*) FROM control_plane.idempotency_receipts receipt WHERE receipt.organization_id=root.organization_id AND receipt.operation='controlplane.complete_execution' AND receipt.idempotency_key=@receipt_key),
 (SELECT count(*) FROM control_plane.audit_events audit WHERE audit.project_id=root.project_id AND audit.action='controlplane.complete_execution'),
 (SELECT count(*) FROM control_plane.run_events event WHERE event.root_run_id=root.id AND event.type='TURN_COMPLETED'),
 (SELECT count(*) FROM control_plane.run_events event WHERE event.root_run_id=root.id AND event.type='ARTIFACT_AVAILABLE'),
 (SELECT count(*) FROM control_plane.callback_receipts callback WHERE callback.child_run_id=root.id),
 (SELECT count(*) FROM control_plane.integration_invocations invocation WHERE invocation.run_id=root.id),
 'platform.artifact.manage'=ANY(agent.capabilities),
 COALESCE(revision.safe_snapshot->'capabilities','[]'::jsonb) ? 'platform.artifact.manage',
 actor.active,lease.fence_digest=@fence_digest,lease.generation=@generation
FROM selected root
JOIN control_plane.runtime_leases lease ON lease.run_id=root.id AND lease.ref=@lease_ref
JOIN control_plane.runtime_revisions revision ON revision.id=lease.runtime_revision_id
JOIN control_plane.run_nodes node ON node.id=lease.node_id
JOIN control_plane.agents agent ON agent.id=node.agent_id
JOIN control_plane.subjects actor ON actor.id=(SELECT run.initiated_by FROM control_plane.runs run WHERE run.id=root.id)
