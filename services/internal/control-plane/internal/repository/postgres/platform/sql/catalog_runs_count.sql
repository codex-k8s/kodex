-- name: catalog_runs_count :one
SELECT count(*)
FROM control_plane.runs r
LEFT JOIN control_plane.projects p ON p.id=r.project_id
JOIN control_plane.sessions s ON s.id=r.session_id
JOIN control_plane.runs root ON root.id=r.root_run_id
LEFT JOIN control_plane.runs parent ON parent.id=r.parent_run_id
LEFT JOIN control_plane.runs retry ON retry.id=r.retry_of_run_id
JOIN control_plane.subjects sub ON sub.id=r.initiated_by
LEFT JOIN control_plane.agents a ON r.target_type IN ('AGENT','SYSTEM_ASSISTANT') AND a.ref=r.target_ref
LEFT JOIN control_plane.workflows w ON r.target_type='WORKFLOW' AND w.ref=r.target_ref
LEFT JOIN control_plane.agents sa ON r.target_type='SYSTEM_ASSISTANT' AND sa.system_key='system-assistant'
LEFT JOIN control_plane.attachment_sets input_attachment_set ON input_attachment_set.id=r.input_attachment_set_id
WHERE r.organization_id=$1::uuid
  AND ($2='' OR p.ref=$2)
  AND ($4='' OR strpos(lower(r.title),lower($4)) > 0 OR strpos(lower(r.task),lower($4)) > 0)
  AND (cardinality($5::text[]) = 0 OR r.state = ANY($5::text[]))
  AND ($6='' OR r.project_id = NULLIF($6,'')::uuid)
  AND EXISTS (SELECT 1 FROM control_plane.catalog_access_targets target
      WHERE target.organization_id=r.organization_id AND target.kind='RUN' AND target.id=r.id
        AND control_plane.catalog_resource_visible(r.organization_id, $3::uuid, 'run.view', target.kind,
            target.id, target.project_id, target.owner_id, target.related_ids, transaction_timestamp()))
