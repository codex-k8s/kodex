-- name: skill_revision_list :one
WITH visible AS MATERIALIZED (
    SELECT revision.id,revision.revision FROM control_plane.skill_bundle_revisions revision
    JOIN control_plane.skill_bundles bundle ON bundle.id=revision.bundle_id
    WHERE bundle.organization_id=@organization_id::uuid AND bundle.ref=@bundle_ref
      AND control_plane.skill_revision_visible(bundle.organization_id,@actor_id::uuid,revision.id,@evaluated_at)
), page AS (
    SELECT * FROM visible WHERE (@before_revision::bigint=0 OR revision<@before_revision::bigint) ORDER BY revision DESC LIMIT @page_size
)
SELECT COALESCE(jsonb_agg(control_plane.skill_revision_projection(page.id) ORDER BY page.revision DESC),'[]'::jsonb),(SELECT count(*) FROM visible) FROM page;
