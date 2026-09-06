-- name: managed_configuration_list :many
WITH visible AS MATERIALIZED (
    SELECT configuration.ref, COALESCE(project.ref, '') AS project_ref, configuration.kind,
           configuration.name, configuration.managed_by, configuration.source,
           configuration.source_revision, configuration.version, configuration.updated_at,
           COALESCE(revision.ref, '') AS revision_ref, COALESCE(revision.revision, 0) AS revision,
           COALESCE(revision.state, '') AS revision_state, COALESCE(revision.digest, '') AS revision_digest,
           COALESCE(recipe.ref, '') AS source_recipe_ref, COALESCE(project.id::text, '') AS source_project_id,
           COALESCE(recipe_owner.ref, project_owner.ref, '') AS source_owner_ref
    FROM control_plane.managed_configuration_sets configuration
    LEFT JOIN control_plane.projects project ON project.id = configuration.project_id
    LEFT JOIN control_plane.subjects project_owner ON project_owner.id = project.created_by
    LEFT JOIN control_plane.managed_role_image_recipes mapping
      ON mapping.configuration_set_id = configuration.id AND mapping.organization_id = configuration.organization_id
    LEFT JOIN control_plane.role_image_recipes recipe
      ON recipe.id = mapping.recipe_id AND recipe.organization_id = configuration.organization_id
    LEFT JOIN control_plane.subjects recipe_owner ON recipe_owner.id = recipe.created_by
    LEFT JOIN control_plane.managed_configuration_revisions revision
      ON revision.id = configuration.current_revision_id AND revision.configuration_set_id = configuration.id
    WHERE configuration.organization_id = @organization_id::uuid
      AND (@project_ref = '' OR project.ref = @project_ref)
      AND (@authority_project = '' OR project.id = NULLIF(@authority_project, '')::uuid)
      AND (@kind = '' OR configuration.kind = @kind)
      AND (@query = '' OR configuration.name ILIKE '%' || @query || '%')
      AND (configuration.project_id IS NULL OR project.lifecycle = 'ACTIVE')
      AND (configuration.kind <> 'EMAIL_MAILBOX' OR EXISTS (
          SELECT 1 FROM control_plane.email_mailbox_configuration_sets mailbox
          JOIN control_plane.integration_connections connection ON connection.id=mailbox.connection_id
              AND connection.organization_id=mailbox.organization_id AND connection.definition_key='email' AND connection.state<>'DELETED'
          WHERE mailbox.configuration_set_id=configuration.id AND mailbox.organization_id=configuration.organization_id
            AND control_plane.catalog_resource_visible(@organization_id::uuid,@actor_id::uuid,'integration.manage',
                'INTEGRATION',connection.id,NULL,connection.created_by,'{}'::jsonb,@evaluated_at)))
      AND control_plane.catalog_resource_visible(@organization_id::uuid, @actor_id::uuid,
          CASE WHEN configuration.project_id IS NULL THEN 'organization.view' ELSE 'project.view' END,
          CASE WHEN configuration.project_id IS NULL THEN 'ORGANIZATION' ELSE 'PROJECT' END,
          COALESCE(project.id, configuration.organization_id), project.id, project.created_by, '{}'::jsonb, @evaluated_at)
), page AS (
    SELECT * FROM visible WHERE ref > @cursor_ref ORDER BY ref LIMIT @page_size
)
SELECT COALESCE(page.ref, ''), COALESCE(page.project_ref, ''), COALESCE(page.kind, ''),
       COALESCE(page.name, ''), COALESCE(page.managed_by, ''), COALESCE(page.source, ''),
       COALESCE(page.source_revision, ''), COALESCE(page.version, 0), COALESCE(page.updated_at, 'epoch'::timestamptz),
       COALESCE(page.revision_ref, ''), COALESCE(page.revision, 0), COALESCE(page.revision_state, ''), COALESCE(page.revision_digest, ''), totals.total,
       COALESCE(page.source_recipe_ref, ''), COALESCE(page.source_project_id, ''), COALESCE(page.source_owner_ref, '')
FROM (SELECT count(*) AS total FROM visible) totals LEFT JOIN page ON true
ORDER BY page.ref;
