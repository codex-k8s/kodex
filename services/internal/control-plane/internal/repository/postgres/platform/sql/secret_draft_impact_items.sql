-- name: secret_draft_impact_items :many
SELECT ref,snapshot,outcome,result_environment_version_ref,result_binding_ref,result_binding_version
FROM control_plane.runtime_secret_draft_impact_items WHERE plan_id=@plan_id::uuid ORDER BY ref;
