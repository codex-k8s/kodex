-- name: prepared_content_bind_revision :exec
INSERT INTO control_plane.prepared_content_bindings(ledger_id,plan_id,plan_revision_id,plan_ref,plan_revision_ref,plan_revision,operation_key)
SELECT content.id,plan.id,revision.id,plan.ref,revision.ref,revision.revision,content.operation_key
FROM control_plane.prepared_content content
JOIN control_plane.assistant_plans plan ON plan.id=content.plan_id
JOIN control_plane.assistant_plan_revisions revision ON revision.id=content.plan_revision_id
WHERE content.id=@id AND content.organization_id=@organization_id AND content.state='STAGED'
ON CONFLICT (ledger_id,plan_ref,plan_revision,operation_key) DO NOTHING;
