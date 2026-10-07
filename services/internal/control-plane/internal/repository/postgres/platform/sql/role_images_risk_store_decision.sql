-- name: role_images_risk_store_decision :one
INSERT INTO control_plane.image_admission_risk_decisions
(ref,organization_id,artifact_id,admission_revision,actor_id,action,reason,decision_json,decision_sha256,risk_acceptance_json,risk_acceptance_sha256,decided_at)
VALUES (@ref,@organization_id::uuid,@artifact_id::uuid,@admission_revision,@actor_id::uuid,@action,@reason,@decision_json,@decision_sha256,@risk_json,@risk_sha256,@decided_at)
RETURNING id::text
