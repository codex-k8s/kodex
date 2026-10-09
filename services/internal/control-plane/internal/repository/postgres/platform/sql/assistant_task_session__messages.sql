-- name: assistant_task_session__messages :many
SELECT event.ref, event.sequence, event.safe_delta->'Message', event.safe_delta->'Execution',
       source.ref, source.version, session.ref, node.ref, turn.ref, turn.turn_number,
       (event.safe_delta->'Execution'->>'RunRef'=source.ref
        AND event.safe_delta->'Execution'->>'SessionRef'=session.ref
        AND event.safe_delta->'Execution'->>'NodeRef'=node.ref
        AND event.safe_delta->'Execution'->>'TurnNumber'=turn.turn_number::text
        AND source.root_run_id=event.root_run_id
        AND ((event.safe_delta->'Message'->>'Phase'='USER'
              AND event.safe_delta->'Message'->>'Ref'=turn.ref
              AND event.safe_delta->'Execution'->>'Attempt' ~ '^[1-9][0-9]{0,8}$')
             OR EXISTS (SELECT 1 FROM control_plane.runtime_revisions revision
                 WHERE revision.organization_id=source.organization_id AND revision.run_id=source.id
                   AND revision.root_run_id=source.root_run_id AND revision.node_id=node.id
                   AND revision.session_id=session.id AND revision.turn_id=turn.id
                   AND revision.attempt::text=event.safe_delta->'Execution'->>'Attempt'))) AS canonical,
       CASE WHEN turn.turn_number=1 AND turn.actor_kind='USER' THEN source.task ELSE turn.content END
FROM control_plane.runs anchor
JOIN control_plane.sessions session ON session.id=anchor.session_id AND session.organization_id=anchor.organization_id
JOIN control_plane.runs source ON source.session_id=session.id AND source.organization_id=session.organization_id
  AND source.project_id IS NOT DISTINCT FROM session.project_id
JOIN control_plane.run_nodes node ON node.run_id=source.id AND node.organization_id=source.organization_id
  AND node.type='AGENT_EXECUTION'
JOIN control_plane.run_events event ON event.root_run_id=source.root_run_id
  AND event.organization_id=source.organization_id AND event.node_ref=node.ref
LEFT JOIN control_plane.session_turns turn ON turn.ref=event.safe_delta->'Execution'->>'TurnRef'
  AND turn.organization_id=source.organization_id AND turn.run_id=source.id AND turn.session_id=session.id
LEFT JOIN control_plane.projects project ON project.id=source.project_id AND project.organization_id=source.organization_id
WHERE anchor.organization_id=@organization_id::uuid AND anchor.ref=@run_ref
  AND event.safe_delta->'Message' IS NOT NULL AND event.safe_delta->'Message'<>'null'::jsonb
  AND (project.id IS NULL OR project.lifecycle='ACTIVE')
  AND (@authority_project_id='' OR source.project_id=NULLIF(@authority_project_id,'')::uuid)
  AND EXISTS (SELECT 1 FROM control_plane.catalog_access_targets target
      WHERE target.organization_id=source.organization_id AND target.kind='RUN' AND target.id=source.id
        AND control_plane.catalog_resource_visible(source.organization_id,@actor_id::uuid,'run.view',
            target.kind,target.id,target.project_id,target.owner_id,target.related_ids,transaction_timestamp()))
ORDER BY event.occurred_at DESC, event.ref DESC
LIMIT 11 OFFSET @offset;
