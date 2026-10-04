-- name: assistant_configuration_component_membership_restore :exec
UPDATE control_plane.access_bindings SET state='ACTIVE'
WHERE organization_id=@organization_id::uuid AND subject_id=@actor_id::uuid AND ref=ANY(@refs::text[]);
