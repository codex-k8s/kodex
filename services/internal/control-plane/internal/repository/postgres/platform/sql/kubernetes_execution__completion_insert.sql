-- name: kubernetes_execution__completion_insert :exec
INSERT INTO control_plane.kubernetes_execution_completions(effect_id,organization_id,outcome,result_json,result_digest,submission_digest,reason_code,applied)
VALUES(@effect_id::uuid,@organization_id::uuid,@outcome,@result_json,@result_digest,@submission_digest,@reason_code,@applied);
