-- name: prompt_continuation_claim_pin :one
SELECT COALESCE(turn.expected_prompt_dependency_digest,''), COALESCE(turn.content,''),
       COALESCE(attachment.ref,''), session.ref, COALESCE(actor.id::text,''), COALESCE(actor.ref,''),
       COALESCE(actor.display_name,''), organization.ref
FROM control_plane.run_nodes node
JOIN control_plane.runs run ON run.id=node.run_id
JOIN control_plane.sessions session ON session.id=run.session_id AND session.organization_id=run.organization_id
JOIN control_plane.organizations organization ON organization.id=run.organization_id
LEFT JOIN control_plane.session_turns turn ON turn.id=node.turn_id AND turn.organization_id=run.organization_id
LEFT JOIN control_plane.attachment_sets attachment ON attachment.id=turn.attachment_set_id
LEFT JOIN control_plane.subjects actor ON actor.ref=COALESCE(turn.actor_ref, '')
  AND actor.organization_id=run.organization_id AND actor.active
WHERE node.organization_id=@organization_id::uuid AND node.ref=@node_ref;
