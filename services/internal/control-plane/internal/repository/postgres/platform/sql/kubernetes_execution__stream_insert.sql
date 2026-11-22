-- name: kubernetes_execution__stream_insert :exec
WITH observed AS MATERIALIZED(SELECT clock_timestamp() AS now)
INSERT INTO control_plane.kubernetes_execution_streams(ref,organization_id,effect_id,generation,state,hard_deadline,maximum_frame_bytes,maximum_total_bytes,input_channels,output_channels,created_at)
SELECT @stream_ref,@organization_id::uuid,@effect_id::uuid,@generation,'PREPARED',@hard_deadline::timestamptz,32768,@maximum_total_bytes::bigint,@input_channels::text[],@output_channels::text[],observed.now
FROM observed WHERE @hard_deadline::timestamptz>observed.now AND @hard_deadline::timestamptz<=observed.now+interval '600 seconds';
