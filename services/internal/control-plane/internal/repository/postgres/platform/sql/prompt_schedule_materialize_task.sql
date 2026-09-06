-- name: prompt_schedule_materialize_task :exec
UPDATE control_plane.session_turns turn SET content=@task
FROM control_plane.runs run,control_plane.run_nodes node
WHERE node.organization_id=@organization_id::uuid AND node.ref=@node_ref
  AND run.id=node.run_id AND run.id=run.root_run_id
  AND turn.id=node.turn_id AND turn.organization_id=node.organization_id
  AND run.source='SCHEDULE';
