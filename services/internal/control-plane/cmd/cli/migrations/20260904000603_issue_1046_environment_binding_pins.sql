-- +goose Up
SET ROLE control_plane_owner;

ALTER TABLE control_plane.runtime_environment_versions
    ADD CONSTRAINT runtime_environment_versions_exact_binding_key
    UNIQUE (organization_id, environment_set_id, id);
ALTER TABLE control_plane.agent_runtime_environment_bindings
    ADD COLUMN environment_version_id uuid;
UPDATE control_plane.agent_runtime_environment_bindings binding
SET environment_version_id = environment.current_version_id
FROM control_plane.runtime_environment_sets environment, control_plane.agents agent
WHERE environment.id = binding.environment_set_id AND agent.id = binding.agent_id
  AND (agent.project_id IS NOT NULL OR agent.system_key IS DISTINCT FROM 'system-assistant');
ALTER TABLE control_plane.agent_runtime_environment_bindings
    ADD CONSTRAINT agent_runtime_environment_binding_exact_version
    FOREIGN KEY (organization_id, environment_set_id, environment_version_id)
    REFERENCES control_plane.runtime_environment_versions (organization_id, environment_set_id, id);

-- +goose StatementBegin
CREATE FUNCTION control_plane.require_runtime_environment_binding_pin()
RETURNS trigger LANGUAGE plpgsql SET search_path = pg_catalog, control_plane AS $$
BEGIN
    IF NEW.environment_version_id IS NULL AND NOT EXISTS (
        SELECT 1 FROM control_plane.agents agent
        WHERE agent.id = NEW.agent_id AND agent.organization_id = NEW.organization_id
          AND agent.project_id IS NULL AND agent.system_key = 'system-assistant'
    ) THEN
        RAISE EXCEPTION 'runtime environment binding requires an exact version';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER runtime_environment_binding_requires_pin
BEFORE INSERT OR UPDATE ON control_plane.agent_runtime_environment_bindings
FOR EACH ROW EXECUTE FUNCTION control_plane.require_runtime_environment_binding_pin();
RESET ROLE;
