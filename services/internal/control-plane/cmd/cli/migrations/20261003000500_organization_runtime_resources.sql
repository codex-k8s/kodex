-- +goose Up
SET ROLE control_plane_owner;

ALTER TABLE control_plane.role_image_recipes
    ADD COLUMN scope_kind text NOT NULL DEFAULT 'PROJECT',
    ALTER COLUMN project_id DROP NOT NULL,
    ADD CONSTRAINT role_image_recipes_scope_check CHECK (
        (scope_kind = 'PROJECT' AND project_id IS NOT NULL) OR
        (scope_kind = 'ORGANIZATION' AND project_id IS NULL)
    );

ALTER TABLE control_plane.image_builds ALTER COLUMN project_id DROP NOT NULL;
ALTER TABLE control_plane.image_artifacts ALTER COLUMN project_id DROP NOT NULL;
ALTER TABLE control_plane.role_image_recipe_revisions ALTER COLUMN project_id DROP NOT NULL;
ALTER TABLE control_plane.role_image_promotion_requests ALTER COLUMN project_id DROP NOT NULL;

ALTER TABLE control_plane.runtime_secrets
    DROP CONSTRAINT runtime_secrets_project_id_name_key,
    ADD COLUMN scope_kind text NOT NULL DEFAULT 'PROJECT',
    ALTER COLUMN project_id DROP NOT NULL,
    ADD CONSTRAINT runtime_secrets_scope_check CHECK (
        (scope_kind = 'PROJECT' AND project_id IS NOT NULL) OR
        (scope_kind = 'ORGANIZATION' AND project_id IS NULL)
    ),
    ADD CONSTRAINT runtime_secrets_scope_name_key
        UNIQUE NULLS NOT DISTINCT (organization_id, project_id, name);

ALTER TABLE control_plane.runtime_secret_operations ALTER COLUMN project_id DROP NOT NULL;

RESET ROLE;

-- Forward-only: организационные runtime-ресурсы после публикации не переводятся
-- обратно в фиктивный проект и не удаляются автоматическим rollback миграции.
-- +goose Down
SELECT 1;
