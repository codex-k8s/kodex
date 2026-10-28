-- name: commit_ack_proof :one
WITH selected AS (SELECT id,organization_id FROM control_plane.projects WHERE ref=@project_ref)
SELECT COALESCE((SELECT min(artifact.ref) FROM control_plane.artifact_heads artifact WHERE artifact.project_id=project.id),''),
 (SELECT count(*) FROM control_plane.artifact_heads artifact WHERE artifact.project_id=project.id),
 (SELECT count(*) FROM control_plane.artifact_revision_content content JOIN control_plane.artifact_revisions revision ON revision.id=content.revision_id JOIN control_plane.artifact_heads artifact ON artifact.id=revision.artifact_id WHERE artifact.project_id=project.id AND content.object_key=@object_key AND content.object_version=@object_version AND content.digest=@digest),
 (SELECT count(*) FROM control_plane.idempotency_receipts receipt WHERE receipt.organization_id=project.organization_id AND receipt.operation=@operation AND receipt.idempotency_key=@receipt_key),
 (SELECT count(*) FROM control_plane.audit_events audit WHERE audit.project_id=project.id AND audit.action=@operation),
 CASE WHEN @operation='artifact.upload' THEN (SELECT count(*) FROM control_plane.outbox_events event WHERE convert_from(event.payload,'UTF8')::jsonb->>'projectRef'=@project_ref AND convert_from(event.payload,'UTF8')::jsonb->>'eventName'='ARTIFACT_CHANGED') ELSE (SELECT count(*) FROM control_plane.run_events event JOIN control_plane.runs root ON root.id=event.root_run_id WHERE root.project_id=project.id AND event.type='ARTIFACT_AVAILABLE') END
FROM selected project
