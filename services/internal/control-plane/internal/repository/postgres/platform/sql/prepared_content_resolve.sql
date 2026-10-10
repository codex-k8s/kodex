-- name: prepared_content_resolve :one
SELECT content.id::text,content.ref,content.generation,content.prepared_artifact_ref,
 content.prepared_revision_ref,content.file_name,content.media_type,content.digest,content.size_bytes,
 content.scan_state,content.preview_state,content.object_key,content.object_version,content.object_etag,
 content.state,content.request_digest
FROM control_plane.prepared_content content
JOIN control_plane.prepared_content_bindings binding ON binding.ledger_id=content.id
JOIN control_plane.assistant_plan_revisions revision ON revision.id=binding.plan_revision_id
JOIN control_plane.assistant_plans plan ON plan.id=binding.plan_id
WHERE content.organization_id=@organization_id AND content.ref=@ref
 AND binding.plan_id=@plan_id AND binding.plan_revision=@plan_revision
 AND revision.plan_id=binding.plan_id AND revision.revision=binding.plan_revision
 AND content.operation_key=@operation_key AND content.digest=@digest AND content.size_bytes=@size_bytes
 AND content.state='STAGED' AND content.adopted_revision_id IS NULL AND content.available_until>clock_timestamp()
 AND plan.current_revision=binding.plan_revision AND plan.state IN ('DRAFT','VALID','INVALID')
 AND EXISTS (SELECT 1 FROM jsonb_array_elements(revision.operations) operation
   WHERE operation->>'ref'=content.operation_key AND operation->'parameters'->>'contentRef'=content.ref
     AND operation->'parameters'->>'digest'=content.digest
     AND operation->'parameters'->>'sizeBytes'=content.size_bytes::text)
FOR UPDATE OF content;
