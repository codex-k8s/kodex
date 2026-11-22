-- name: kubernetes_capture__artifact_create :one
WITH ids AS MATERIALIZED(SELECT gen_random_uuid() AS artifact_id,gen_random_uuid() AS revision_id),
head AS(INSERT INTO control_plane.artifact_heads(id,ref,organization_id,project_id,run_id,node_id,version,created_by,current_revision_id)
 SELECT artifact_id,@artifact_ref,@organization_id::uuid,nullif(@project_id,'')::uuid,@run_id::uuid,@node_id::uuid,1,@actor_id::uuid,revision_id FROM ids
 RETURNING id,current_revision_id)
INSERT INTO control_plane.artifact_revisions(id,ref,artifact_id,revision,file_name,media_type,size_bytes,digest,source,scan_state,object_receipt_ref,preview_state,created_by,created_at)
SELECT head.current_revision_id,@revision_ref,head.id,1,@file_name,@media_type,@size_bytes,@digest,'INTEGRATION_RESULT',@scan_state,@receipt_ref,@preview_state,@actor_id::uuid,clock_timestamp() FROM head
RETURNING artifact_id::text,id::text,revision;
