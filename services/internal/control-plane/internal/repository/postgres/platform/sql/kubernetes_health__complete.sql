-- name: kubernetes_health__complete :exec
INSERT INTO control_plane.kubernetes_identity_health_completions(attempt_id,outcome,subject_digest,state,applied)
VALUES(@attempt_id::uuid,@outcome,NULLIF(@subject_digest,''),@state,@applied);
