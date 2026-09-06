-- name: provider_account_blockers_page :one
WITH dependencies AS MATERIALIZED (
    SELECT blocker.*, COALESCE(project.ref, '') AS project_ref,
        (@authority_project = '' OR blocker.project_id IS NULL OR blocker.project_id::text = @authority_project)
        AND control_plane.catalog_resource_visible(
            @organization_id::uuid, @actor_id::uuid,
            CASE blocker.resource_kind WHEN 'AGENT' THEN 'agent.view'
                WHEN 'SCHEDULE' THEN 'schedule.view' ELSE 'run.view' END,
            blocker.resource_kind, blocker.resource_id, blocker.project_id, blocker.owner_id,
            CASE WHEN blocker.project_id IS NULL THEN '{}'::jsonb
                ELSE jsonb_build_object('PROJECT', blocker.project_id::text) END,
            transaction_timestamp()) AS visible,
        blocker.kind = 'QUEUED_TURN'
        AND control_plane.provider_queued_run_cancellable(@organization_id::uuid,blocker.resource_id)
        AND control_plane.catalog_resource_visible(
            @organization_id::uuid, @actor_id::uuid, 'run.cancel',
            blocker.resource_kind, blocker.resource_id, blocker.project_id, blocker.owner_id,
            jsonb_build_object('PROJECT', blocker.project_id::text), transaction_timestamp()) AS can_cancel
    FROM control_plane.provider_account_blockers(@organization_id::uuid, @account_id::uuid) blocker
    LEFT JOIN control_plane.projects project ON project.id = blocker.project_id
        AND project.organization_id = @organization_id::uuid
), filtered AS MATERIALIZED (
    SELECT * FROM dependencies
    WHERE (@kind = '' OR kind = @kind)
      AND (NOT visible OR @query = '' OR strpos(lower(name), lower(@query)) > 0)
), page AS (
    SELECT *, kind || '/' || ref AS cursor_key
    FROM filtered WHERE visible AND kind || '/' || ref > @after_key
    ORDER BY kind, ref LIMIT @page_size
)
SELECT COALESCE((SELECT jsonb_agg(jsonb_build_object(
            'Kind', kind, 'Ref', ref, 'Version', version, 'Name', name,
            'ProjectRef', project_ref, 'CanCancel', can_cancel) ORDER BY kind, ref) FROM page), '[]'::jsonb),
       (SELECT count(*) FROM filtered WHERE visible),
       (SELECT count(*) FROM filtered WHERE NOT visible),
       (SELECT encode(sha256(convert_to(COALESCE(string_agg(
            kind || '/' || ref || ':' || version::text || ':' || source_pin || ':' || visible::text || ':' || can_cancel::text,
            '|' ORDER BY kind, ref), ''), 'UTF8')), 'hex') FROM dependencies);
