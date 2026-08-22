-- name: platform__queries_listownergates_select_owner_gates_organization_id_ref_state :many
SELECT g.ref,p.ref,root.ref,n.ref,g.title,g.prompt,g.context_summary,g.allowed_decisions,g.state,COALESCE(g.decision,''),g.decision_comment,COALESCE(s.display_name,''),g.version,g.created_at,g.resolved_at
FROM control_plane.owner_gates g
JOIN control_plane.projects p ON p.id=g.project_id
JOIN control_plane.runs root ON root.id=g.root_run_id
JOIN control_plane.run_nodes n ON n.id=g.node_id
LEFT JOIN control_plane.subjects s ON s.id=g.resolved_by
WHERE g.organization_id=$1::uuid
  AND ($2='' OR p.ref=$2)
  AND ($3='' OR g.state=$3)
  AND ($4 IN ('OWNER','ADMINISTRATOR') OR EXISTS(
    SELECT 1 FROM control_plane.memberships m
    WHERE m.project_id=g.project_id AND m.subject_id=$5::uuid AND m.active AND 'VIEW'=ANY(m.permissions)
  ))
ORDER BY g.created_at DESC
LIMIT $6
