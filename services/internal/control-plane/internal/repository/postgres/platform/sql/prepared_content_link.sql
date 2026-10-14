-- name: prepared_content_link :exec
UPDATE control_plane.prepared_content content
SET plan_id=plan.id,plan_revision_id=revision.id,plan_revision=revision.revision,
 origin_plan_ref=coalesce(content.origin_plan_ref,plan.ref),
 origin_conversation_ref=coalesce(content.origin_conversation_ref,plan.conversation_ref),
 origin_plan_revision_ref=coalesce(content.origin_plan_revision_ref,revision.ref),
 origin_plan_revision=coalesce(content.origin_plan_revision,revision.revision),updated_at=clock_timestamp()
FROM control_plane.assistant_plans plan
JOIN control_plane.assistant_plan_revisions revision ON revision.plan_id=plan.id
WHERE content.id=@id AND content.organization_id=@organization_id
 AND plan.organization_id=content.organization_id AND plan.id=@plan_id AND revision.revision=@plan_revision
 AND revision.organization_id=content.organization_id AND plan.current_revision=revision.revision
 AND content.operation_key=@operation_key AND content.state='STAGED' AND content.available_until>clock_timestamp()
 AND (content.plan_id IS NULL OR (content.plan_id=plan.id AND content.plan_revision<=revision.revision))
 AND EXISTS (SELECT 1 FROM jsonb_array_elements(revision.operations) operation
   WHERE operation->>'ref'=content.operation_key AND operation->'parameters'->>'contentRef'=content.ref
     AND operation->'parameters'->>'digest'=content.digest
     AND operation->'parameters'->>'sizeBytes'=content.size_bytes::text);
