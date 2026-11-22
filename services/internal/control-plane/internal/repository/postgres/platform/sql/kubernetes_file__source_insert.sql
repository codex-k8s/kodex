-- name: kubernetes_file__source_insert :one
INSERT INTO control_plane.kubernetes_file_input_sources(ref,organization_id,invocation_id,child_id,plan_id,actor_id,project_id,
 snapshot_digest,binding_digest,child_input_digest,invocation_generation,original_fence_digest,state,maximum_bytes,transfer_format,hard_deadline,writer_deadline)
VALUES(@source_ref,@organization_id::uuid,@invocation_id::uuid,@child_id::uuid,@plan_id::uuid,@actor_id::uuid,@project_id::uuid,
 @snapshot_digest,@binding_digest,@child_input_digest,@generation,@fence_digest,'PREPARING',@maximum_bytes,@transfer_format,@hard_deadline,@writer_deadline)
RETURNING id::text;
