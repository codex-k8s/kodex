-- name: kubernetes_execution__effect_insert :one
WITH observed AS MATERIALIZED(SELECT clock_timestamp() AS now)
INSERT INTO control_plane.kubernetes_execution_effects(ref,organization_id,child_id,generation,state,admission_expires_at,created_at)
SELECT @effect_ref,@organization_id::uuid,@child_id::uuid,1,'ATTEMPTING',
 least(observed.now+interval '30 seconds',@authorized_until::timestamptz),observed.now FROM observed
WHERE @authorized_until::timestamptz>observed.now
RETURNING id::text,admission_expires_at;
