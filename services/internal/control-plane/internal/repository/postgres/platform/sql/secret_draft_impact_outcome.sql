-- name: secret_draft_impact_outcome :exec
UPDATE control_plane.runtime_secret_draft_impact_items
SET outcome=@outcome,result_environment_version_ref=@environment_version_ref,result_binding_ref=@binding_ref,result_binding_version=@binding_version
WHERE plan_id=@plan_id::uuid AND ref=@item_ref AND outcome='PENDING';
