-- name: kubernetes_execution__frame_advance :exec
UPDATE control_plane.kubernetes_execution_streams
SET input_sequence=CASE WHEN @direction='INPUT' THEN @sequence ELSE input_sequence END,
 output_sequence=CASE WHEN @direction='OUTPUT' THEN @sequence ELSE output_sequence END,
 logical_bytes=logical_bytes+@byte_count,delivered_bytes=delivered_bytes+@byte_count,
 stdin_closed=stdin_closed OR @close_channel='STDIN',
 closed_input_channels=CASE WHEN @close_channel='' THEN closed_input_channels ELSE array_append(closed_input_channels,@close_channel::text) END
WHERE id=@stream_id::uuid AND organization_id=@organization_id::uuid;
