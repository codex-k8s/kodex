-- name: queries_attachconversation_select_session_turns_organization_id_session_id_ref :many
SELECT t.ref,t.turn_number,t.actor_kind,COALESCE(s.display_name,a.name,t.actor_ref),t.content,
       CASE
         WHEN t.actor_kind = 'USER' AND run.state = 'CANCELLED' THEN 'CANCELLED'
         WHEN t.actor_kind = 'USER' AND node.state = 'QUEUED' THEN 'QUEUED'
         WHEN t.actor_kind = 'USER' AND node.state IN ('RUNNING','WAITING') THEN 'RUNNING'
         ELSE t.state
       END,
       COALESCE(attachment_set.ref,''),t.created_at,t.completed_at,
       COALESCE(run.ref,''),COALESCE(run.version,0)
FROM control_plane.session_turns t
LEFT JOIN control_plane.subjects s ON t.actor_kind='USER' AND s.ref=t.actor_ref
LEFT JOIN control_plane.agents a ON t.actor_kind<>'USER' AND a.ref=t.actor_ref
LEFT JOIN control_plane.attachment_sets attachment_set ON attachment_set.id=t.attachment_set_id
LEFT JOIN control_plane.runs run ON run.id=t.run_id
LEFT JOIN control_plane.run_nodes node ON node.turn_id=t.id AND node.run_id=t.run_id AND node.type='AGENT_EXECUTION'
WHERE t.organization_id=$1::uuid
  AND t.session_id=(SELECT session_id FROM control_plane.assistant_conversations WHERE ref=$2)
ORDER BY t.turn_number
