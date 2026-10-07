-- name: runtime_activity__execution :one
SELECT r.ref, n.ref, s.ref, t.ref, t.turn_number, n.attempt,
  CASE WHEN t.turn_number=1 AND t.actor_kind='USER' THEN r.task ELSE t.content END,
  t.actor_kind, t.actor_ref, COALESCE(actor_subject.display_name, actor_agent.name, 'Kodex')
FROM control_plane.run_nodes n
JOIN control_plane.runs r ON r.id = n.run_id
JOIN control_plane.session_turns t ON t.id = n.turn_id AND t.organization_id = n.organization_id
JOIN control_plane.sessions s ON s.id = t.session_id AND s.organization_id = n.organization_id
LEFT JOIN control_plane.subjects actor_subject ON actor_subject.ref = t.actor_ref AND actor_subject.organization_id = n.organization_id
LEFT JOIN control_plane.agents actor_agent ON actor_agent.ref = t.actor_ref AND actor_agent.organization_id = n.organization_id
WHERE n.organization_id = @organization_id::uuid AND n.ref = @node_ref
  AND r.root_run_id = @root_run_id::uuid
