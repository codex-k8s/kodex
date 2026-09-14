-- name: catalog_runs_count :one
SELECT count(*)
FROM control_plane.runs r
LEFT JOIN control_plane.projects p ON p.id=r.project_id
WHERE r.organization_id=$1::uuid
  AND ($2='' OR p.ref=$2)
  AND ($5='' OR strpos(lower(r.title),lower($5)) > 0 OR strpos(lower(r.task),lower($5)) > 0)
  AND (cardinality($6::text[]) = 0 OR r.state = ANY($6::text[]))
  AND ($7='' OR r.project_id = NULLIF($7,'')::uuid)
  AND ($3 IN ('OWNER','ADMINISTRATOR') OR EXISTS (SELECT 1 FROM control_plane.catalog_access_targets target
      WHERE target.organization_id=r.organization_id AND target.kind='RUN' AND target.id=r.id
        AND control_plane.catalog_resource_visible(r.organization_id, $4::uuid, 'run.view', target.kind,
            target.id, target.project_id, target.owner_id, target.related_ids, transaction_timestamp())))
