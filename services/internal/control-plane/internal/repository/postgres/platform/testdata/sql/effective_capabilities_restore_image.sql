-- Возвращает только binding собственного disposable Agent; immutable rows сохраняются.
UPDATE control_plane.agent_runtime_environment_bindings binding
SET environment_version_id = version.parent_version_id
FROM control_plane.runtime_environment_versions version, control_plane.agents agent
WHERE binding.environment_version_id = version.id AND agent.id = binding.agent_id
  AND agent.organization_id = $1::uuid AND agent.ref = $2 AND version.ref = $3;
