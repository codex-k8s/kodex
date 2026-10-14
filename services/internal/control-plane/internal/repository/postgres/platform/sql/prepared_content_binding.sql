-- name: prepared_content_binding :one
SELECT content.actor_id::text,content.organization_id::text,organization.ref,content.project_id::text,content.origin_project_ref,
 content.source_profile,content.source_profile_ref,content.source_context_digest,content.source_lease_ref,
 content.source_fence_digest,content.source_run_ref,content.operation_key,content.intent_operation,content.idempotency_key,content.intent_digest,
 coalesce(content.target_artifact_id::text,''),content.target_artifact_ref,content.source_revision_ref,
 content.source_profile_version,content.source_lease_generation,content.expected_artifact_version
FROM control_plane.prepared_content content
JOIN control_plane.organizations organization ON organization.id=content.organization_id
JOIN control_plane.prepared_content_bindings binding ON binding.ledger_id=content.id
JOIN control_plane.assistant_plans plan ON plan.id=binding.plan_id
JOIN control_plane.assistant_plan_revisions revision ON revision.id=binding.plan_revision_id
WHERE content.organization_id=@organization_id AND content.ref=@ref
 AND content.project_id IS NOT NULL AND binding.plan_id=@plan_id AND binding.plan_revision=@plan_revision
 AND plan.current_revision=binding.plan_revision AND plan.state IN ('DRAFT','VALID','INVALID','STALE')
 AND revision.plan_id=plan.id AND revision.revision=binding.plan_revision
 AND content.operation_key=@operation_key AND content.digest=@digest AND content.size_bytes=@size_bytes
 AND EXISTS (SELECT 1 FROM jsonb_array_elements(revision.operations) operation
   WHERE operation->>'ref'=content.operation_key AND operation->'parameters'->>'contentRef'=content.ref
     AND operation->'parameters'->>'digest'=content.digest
     AND operation->'parameters'->>'sizeBytes'=content.size_bytes::text)
FOR SHARE OF content;
