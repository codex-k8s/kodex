-- name: revision_impact__items :many
SELECT snapshot,outcome,result_revision_ref,result_binding_ref,result_binding_version,result_consumer_version
FROM control_plane.revision_impact_items WHERE plan_id=$1::uuid ORDER BY ref;
