-- name: kubernetes_capture__artifact_origin :exec
INSERT INTO control_plane.kubernetes_capture_artifact_revisions
(organization_id,capture_id,stream_id,channel,artifact_id,revision_id,artifact_ref,revision_ref,prepared_content_id,digest,size_bytes,source_digest,origin_json)
VALUES(@organization_id::uuid,@capture_id::uuid,@stream_id::uuid,@channel,@artifact_id::uuid,@revision_id::uuid,@artifact_ref,@revision_ref,@prepared_content_id::uuid,@digest,@size_bytes,@source_digest,@origin_json);
