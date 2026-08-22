-- name: platform__bootstrap_component_replace_core_prompt :exec
UPDATE control_plane.instruction_versions
SET content = 'replacement is forbidden'
WHERE core;
