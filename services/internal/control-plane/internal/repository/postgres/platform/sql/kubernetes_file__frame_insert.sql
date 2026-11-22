-- name: kubernetes_file__frame_insert :exec
INSERT INTO control_plane.kubernetes_file_input_frames(source_id,stream_id,organization_id,stage_id,ordinal,byte_offset,byte_count,payload_digest,end_of_input)
VALUES(@source_id::uuid,@stream_id::uuid,@organization_id::uuid,@stage_id::uuid,@ordinal,@byte_offset,@byte_count,@payload_digest,@end_of_input);
