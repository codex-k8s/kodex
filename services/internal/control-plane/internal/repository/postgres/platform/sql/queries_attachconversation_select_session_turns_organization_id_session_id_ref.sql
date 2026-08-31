-- name: queries_attachconversation_select_session_turns_organization_id_session_id_ref :many
SELECT t.ref,t.turn_number,t.actor_kind,COALESCE(s.display_name,a.name,t.actor_ref),t.content,t.state,
       COALESCE(attachment_set.ref,''),t.created_at,t.completed_at
FROM control_plane.session_turns t
LEFT JOIN control_plane.subjects s ON t.actor_kind='USER' AND s.ref=t.actor_ref
LEFT JOIN control_plane.agents a ON t.actor_kind<>'USER' AND a.ref=t.actor_ref
LEFT JOIN control_plane.attachment_sets attachment_set ON attachment_set.id=t.attachment_set_id
WHERE t.organization_id=$1::uuid
  AND t.session_id=(SELECT session_id FROM control_plane.assistant_conversations WHERE ref=$2)
ORDER BY t.turn_number
