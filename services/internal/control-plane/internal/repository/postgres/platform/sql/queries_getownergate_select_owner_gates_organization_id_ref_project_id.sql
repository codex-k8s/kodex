-- name: queries_getownergate_select_owner_gates_organization_id_ref_project_id :one
SELECT g.ref,p.ref,root.ref,n.ref,g.title,g.prompt,g.context_summary,COALESCE(requester_agent.ref,initiator.ref),COALESCE(requester_agent.name,initiator.display_name),g.allowed_decisions,g.state,COALESCE(g.decision,''),g.decision_comment,COALESCE(s.display_name,''),g.version,g.created_at,g.resolved_at,COALESCE(attachment_set.ref,''),
       ($3 IN ('OWNER','ADMINISTRATOR') OR EXISTS(
         SELECT 1 FROM control_plane.memberships resolve_membership
         WHERE resolve_membership.project_id=g.project_id AND resolve_membership.subject_id=$4::uuid AND resolve_membership.active AND 'RESOLVE_GATES'=ANY(resolve_membership.permissions)
       ))
FROM control_plane.owner_gates g
LEFT JOIN control_plane.attachment_sets attachment_set ON attachment_set.id=g.resolution_attachment_set_id
JOIN control_plane.projects p ON p.id=g.project_id
JOIN control_plane.runs root ON root.id=g.root_run_id
JOIN control_plane.run_nodes n ON n.id=g.node_id
JOIN control_plane.subjects initiator ON initiator.id=root.initiated_by
LEFT JOIN control_plane.run_nodes requester_node ON requester_node.id=n.parent_node_id
LEFT JOIN control_plane.agents requester_agent ON requester_agent.id=requester_node.agent_id
LEFT JOIN control_plane.subjects s ON s.id=g.resolved_by
WHERE g.organization_id=$1::uuid
  AND g.ref=$2
  AND ($3 IN ('OWNER','ADMINISTRATOR') OR EXISTS(
    SELECT 1 FROM control_plane.memberships m
    WHERE m.project_id=g.project_id AND m.subject_id=$4::uuid AND m.active AND 'VIEW'=ANY(m.permissions)
  ))
