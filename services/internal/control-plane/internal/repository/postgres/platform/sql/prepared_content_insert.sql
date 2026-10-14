-- name: prepared_content_insert :one
INSERT INTO control_plane.prepared_content
 (ref,organization_id,project_id,origin_project_ref,actor_id,intent_operation,idempotency_key,intent_digest,request_digest,
  operation_key,source_profile,source_profile_ref,source_profile_version,source_context_digest,
  source_lease_ref,source_lease_generation,source_fence_digest,source_run_ref,target_artifact_id,
  target_artifact_ref,source_revision_ref,expected_artifact_version,prepared_artifact_ref,prepared_revision_ref,
  file_name,media_type,digest,size_bytes,scan_state,preview_state,object_key)
VALUES (@ref,@organization_id,@project_id,@origin_project_ref,@actor_id,@intent_operation,@idempotency_key,@intent_digest,@request_digest,
  @operation_key,@source_profile,@source_profile_ref,@source_profile_version,@source_context_digest,
  @source_lease_ref,@source_lease_generation,@source_fence_digest,@source_run_ref,@target_artifact_id,
  @target_artifact_ref,@source_revision_ref,@expected_artifact_version,@prepared_artifact_ref,@prepared_revision_ref,
  @file_name,@media_type,@digest,@size_bytes,@scan_state,@preview_state,@object_key)
ON CONFLICT (organization_id,actor_id,intent_operation,idempotency_key,operation_key) DO NOTHING
RETURNING id::text;
