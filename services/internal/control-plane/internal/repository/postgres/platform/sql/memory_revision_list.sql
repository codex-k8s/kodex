-- name: memory_revision_list :one
WITH visible AS MATERIALIZED (
    SELECT revision.id,revision.revision FROM control_plane.memory_record_revisions revision
    JOIN control_plane.memory_records record ON record.id=revision.record_id
    WHERE record.organization_id=@organization_id::uuid AND record.ref=@record_ref
      AND (revision.source_run_id IS NULL OR EXISTS (
          SELECT 1 FROM control_plane.catalog_access_targets source WHERE source.organization_id=record.organization_id AND source.kind='RUN' AND source.id=revision.source_run_id
          AND control_plane.catalog_resource_visible(record.organization_id,@actor_id::uuid,'run.view','RUN',source.id,source.project_id,source.owner_id,source.related_ids,@evaluated_at,false)))
), page AS (
    SELECT * FROM visible WHERE (@before_revision::bigint=0 OR revision<@before_revision::bigint) ORDER BY revision DESC LIMIT @page_size
)
SELECT COALESCE(jsonb_agg(control_plane.memory_revision_projection(page.id) ORDER BY page.revision DESC),'[]'::jsonb),(SELECT count(*) FROM visible) FROM page;
