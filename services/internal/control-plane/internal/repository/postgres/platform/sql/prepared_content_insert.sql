-- name: prepared_content_insert :one
WITH project_boundary AS MATERIALIZED (
 SELECT project.id FROM control_plane.projects project
 WHERE project.id=@project_id::uuid AND project.organization_id=@organization_id::uuid
  AND project.lifecycle='ACTIVE' AND @source_profile IN('KUBERNETES_OUTPUT','KUBERNETES_FILE_SOURCE')
 FOR SHARE OF project
)
INSERT INTO control_plane.prepared_content
 (ref,organization_id,project_id,origin_project_ref,actor_id,intent_operation,idempotency_key,intent_digest,request_digest,
  operation_key,source_profile,source_profile_ref,source_profile_version,source_context_digest,
  source_lease_ref,source_lease_generation,source_fence_digest,source_run_ref,target_artifact_id,
  target_artifact_ref,source_revision_ref,expected_artifact_version,prepared_artifact_ref,prepared_revision_ref,
  file_name,media_type,digest,size_bytes,scan_state,preview_state,object_key,antivirus_report,available_until)
SELECT @ref,@organization_id,@project_id,@origin_project_ref,@actor_id,@intent_operation,@idempotency_key,@intent_digest,@request_digest,
  @operation_key,@source_profile,@source_profile_ref,@source_profile_version,@source_context_digest,
  @source_lease_ref,@source_lease_generation,@source_fence_digest,@source_run_ref,@target_artifact_id,
  @target_artifact_ref,@source_revision_ref,@expected_artifact_version,@prepared_artifact_ref,@prepared_revision_ref,
  @file_name,@media_type,@digest,@size_bytes,@scan_state,@preview_state,@object_key,@antivirus_report,
  CASE WHEN @source_profile IN('KUBERNETES_OUTPUT','KUBERNETES_FILE_SOURCE') THEN 'infinity'::timestamptz ELSE clock_timestamp()+interval '24 hours' END
WHERE @source_profile NOT IN('KUBERNETES_OUTPUT','KUBERNETES_FILE_SOURCE')
 OR @project_id::uuid IS NULL OR EXISTS(SELECT 1 FROM project_boundary)
ON CONFLICT (organization_id,actor_id,intent_operation,idempotency_key,operation_key) DO NOTHING
RETURNING id::text;
