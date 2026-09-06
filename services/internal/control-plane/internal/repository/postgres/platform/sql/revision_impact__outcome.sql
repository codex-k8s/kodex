-- name: revision_impact__outcome :exec
UPDATE control_plane.revision_impact_items SET outcome=@outcome,result_revision_ref=@revision_ref,
 result_binding_ref=@binding_ref,result_binding_version=@binding_version,result_consumer_version=@consumer_version
WHERE plan_id=@plan_id::uuid AND ref=@ref AND outcome='PENDING';
