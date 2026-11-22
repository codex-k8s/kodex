-- name: kubernetes_file__source :one
SELECT source.id::text,source.ref,invocation.ref,plan.ref,child.ref,coalesce(stream.ref,''),source.state,source.version,
 origin.artifact_ref,origin.artifact_revision_ref,substring(origin.artifact_digest FROM 8),origin.artifact_size_bytes,
 source.transfer_format,coalesce(source.payload_digest,''),coalesce(source.byte_count,0),coalesce(source.normalization_revision,0),
 coalesce((SELECT sum(frame.byte_count) FROM control_plane.kubernetes_file_input_frames frame WHERE frame.source_id=source.id),0),
 coalesce((SELECT sum(frame.byte_count) FROM control_plane.kubernetes_file_input_frames frame JOIN control_plane.kubernetes_input_stages stage ON stage.id=frame.stage_id AND stage.organization_id=frame.organization_id WHERE frame.source_id=source.id AND stage.state='ACKNOWLEDGED'),0),
 EXISTS(SELECT 1 FROM control_plane.kubernetes_file_input_frames frame JOIN control_plane.kubernetes_input_stages stage ON stage.id=frame.stage_id AND stage.organization_id=frame.organization_id WHERE frame.source_id=source.id AND frame.end_of_input AND stage.state='ACKNOWLEDGED'),
 source.reason_code,source.created_at,source.hard_deadline,source.writer_deadline,source.authorized_until,source.source_access_until,
 coalesce(source.prepared_content_id::text,''),coalesce(source.manifest,''),coalesce(source.manifest_digest,''),
 coalesce(source.object_key,''),coalesce(source.object_version,''),coalesce(source.object_etag,''),source.payload_purged_at,
 source.maximum_bytes,source.invocation_generation,source.original_fence_digest,source.snapshot_digest,source.binding_digest,source.child_input_digest
FROM control_plane.kubernetes_file_input_sources source
JOIN control_plane.kubernetes_file_input_origins origin ON origin.invocation_id=source.invocation_id AND origin.organization_id=source.organization_id
JOIN control_plane.integration_invocations invocation ON invocation.id=source.invocation_id AND invocation.organization_id=source.organization_id
JOIN control_plane.kubernetes_execution_plans plan ON plan.id=source.plan_id AND plan.organization_id=source.organization_id
JOIN control_plane.kubernetes_execution_children child ON child.id=source.child_id AND child.organization_id=source.organization_id
LEFT JOIN control_plane.kubernetes_execution_streams stream ON stream.id=source.stream_id AND stream.organization_id=source.organization_id
WHERE source.organization_id=@organization_id::uuid AND invocation.ref=@invocation_ref
 AND(@source_ref='' OR source.ref=@source_ref) AND(@child_ref='' OR child.ref=@child_ref)
FOR UPDATE OF source;
