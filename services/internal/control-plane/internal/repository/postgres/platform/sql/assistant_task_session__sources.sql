-- name: assistant_task_session__sources :many
SELECT session.ref, session.version, session.state, COALESCE(storage.state, 'UNTRACKED'),
       source.ref, source.version, root.ref,
       COALESCE((SELECT max(event.sequence)
           FROM control_plane.run_nodes node
           JOIN control_plane.run_events event ON event.organization_id=node.organization_id
             AND event.root_run_id=source.root_run_id AND event.node_ref=node.ref
           WHERE node.organization_id=source.organization_id AND node.run_id=source.id
             AND event.safe_delta->'Message' IS NOT NULL
             AND event.safe_delta->'Message'<>'null'::jsonb),0)
FROM control_plane.runs anchor
JOIN control_plane.sessions session ON session.id=anchor.session_id AND session.organization_id=anchor.organization_id
LEFT JOIN control_plane.session_storage storage ON storage.session_id=session.id AND storage.organization_id=session.organization_id
JOIN control_plane.runs source ON source.session_id=session.id AND source.organization_id=session.organization_id
  AND source.project_id IS NOT DISTINCT FROM session.project_id
JOIN control_plane.runs root ON root.id=source.root_run_id AND root.organization_id=source.organization_id
LEFT JOIN control_plane.projects project ON project.id=source.project_id AND project.organization_id=source.organization_id
WHERE anchor.organization_id=@organization_id::uuid AND anchor.ref=@run_ref
  AND (project.id IS NULL OR project.lifecycle='ACTIVE')
  AND (@authority_project_id='' OR source.project_id=NULLIF(@authority_project_id,'')::uuid)
  AND EXISTS (SELECT 1 FROM control_plane.catalog_access_targets target
      WHERE target.organization_id=source.organization_id AND target.kind='RUN' AND target.id=source.id
        AND control_plane.catalog_resource_visible(source.organization_id,@actor_id::uuid,'run.view',
            target.kind,target.id,target.project_id,target.owner_id,target.related_ids,transaction_timestamp()))
ORDER BY source.ref
LIMIT 129;
