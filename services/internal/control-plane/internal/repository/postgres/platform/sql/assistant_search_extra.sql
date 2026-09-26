-- name: assistant_search_extra :many
WITH matches AS (
    SELECT 'ROLE_IMAGE'::text AS kind, recipe.ref, project.ref AS project_ref,
           recipe.name AS title, ''::text AS subtitle, recipe.state,
           recipe.updated_at, recipe.created_at AS order_time,
           CASE WHEN lower(recipe.name) = lower(@query) THEN 0
                WHEN recipe.name ILIKE @query || '%' THEN 1 ELSE 2 END AS relevance
    FROM control_plane.role_image_recipes AS recipe
    JOIN control_plane.projects AS project ON project.id = recipe.project_id
      AND project.lifecycle NOT IN ('TRASHED', 'PURGE_PENDING')
    WHERE recipe.organization_id = @organization_id::uuid AND recipe.state = 'ACTIVE'
      AND (@project_ref = '' OR project.ref = @project_ref)
      AND recipe.name ILIKE '%' || @query || '%'
    UNION ALL
    SELECT 'RUNTIME_ENVIRONMENT', environment.ref, project.ref,
           environment.name, environment.description, environment.state,
           environment.updated_at, environment.created_at,
           CASE WHEN lower(environment.name) = lower(@query) THEN 0
                WHEN environment.name ILIKE @query || '%' THEN 1 ELSE 2 END
    FROM control_plane.runtime_environment_sets AS environment
    JOIN control_plane.projects AS project ON project.id = environment.project_id
      AND project.lifecycle NOT IN ('TRASHED', 'PURGE_PENDING')
    WHERE environment.organization_id = @organization_id::uuid AND environment.state <> 'DELETED'
      AND (@project_ref = '' OR project.ref = @project_ref)
      AND (environment.name ILIKE '%' || @query || '%' OR environment.description ILIKE '%' || @query || '%')
    UNION ALL
    SELECT 'SCHEDULE', schedule.ref, project.ref, schedule.name, ''::text,
           schedule.lifecycle_state, schedule.updated_at, schedule.created_at,
           CASE WHEN lower(schedule.name) = lower(@query) THEN 0
                WHEN schedule.name ILIKE @query || '%' THEN 1 ELSE 2 END
    FROM control_plane.schedules AS schedule
    JOIN control_plane.projects AS project ON project.id = schedule.project_id
      AND project.lifecycle NOT IN ('TRASHED', 'PURGE_PENDING')
    WHERE schedule.organization_id = @organization_id::uuid AND schedule.lifecycle_state <> 'DELETED'
      AND (@project_ref = '' OR project.ref = @project_ref)
      AND schedule.name ILIKE '%' || @query || '%'
    UNION ALL
    SELECT 'SECRET', secret.ref, project.ref, secret.name, ''::text,
           secret.state, secret.updated_at, secret.created_at,
           CASE WHEN lower(secret.name) = lower(@query) THEN 0
                WHEN secret.name ILIKE @query || '%' THEN 1 ELSE 2 END
    FROM control_plane.runtime_secrets AS secret
    JOIN control_plane.projects AS project ON project.id = secret.project_id
      AND project.lifecycle NOT IN ('TRASHED', 'PURGE_PENDING')
    WHERE secret.organization_id = @organization_id::uuid AND secret.state <> 'PROVISIONING'
      AND (@project_ref = '' OR project.ref = @project_ref)
      AND secret.name ILIKE '%' || @query || '%'
    UNION ALL
    SELECT 'INTEGRATION', connection.ref, ''::text, connection.name, definition.name,
           connection.state, connection.updated_at, connection.created_at,
           CASE WHEN lower(connection.name) = lower(@query) THEN 0
                WHEN connection.name ILIKE @query || '%' THEN 1 ELSE 2 END
    FROM control_plane.integration_connections AS connection
    JOIN control_plane.integration_definitions AS definition ON definition.stable_key = connection.definition_key
    WHERE connection.organization_id = @organization_id::uuid AND connection.lifecycle_state = 'ACTIVE'
      AND @project_ref = ''
      AND (connection.name ILIKE '%' || @query || '%' OR definition.name ILIKE '%' || @query || '%')
)
SELECT match.kind, match.ref, match.project_ref, match.title, match.subtitle,
       match.state, match.updated_at, match.relevance, match.order_time
FROM matches AS match
ORDER BY match.relevance, match.order_time DESC, match.kind, match.ref;
