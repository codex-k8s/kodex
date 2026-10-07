-- name: environment_initial_system_binding_fixture :one
WITH changed AS (
    UPDATE control_plane.agent_runtime_environment_bindings binding
    SET environment_version_id=NULL, version=binding.version+CASE WHEN $2::boolean THEN 1 ELSE 0 END
    FROM control_plane.agents agent
    WHERE agent.id=binding.agent_id AND agent.ref=$1
    RETURNING agent.id
), bumped AS (
    UPDATE control_plane.agents agent SET version=version+1
    WHERE $2::boolean AND agent.id IN (SELECT id FROM changed)
)
SELECT count(*) FROM changed;
