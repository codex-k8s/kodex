-- name: commands_emitrunevent_select_gate_delta :one
SELECT gate.ref,
       project.ref,
       root.ref,
       node.ref,
       gate.title,
       gate.prompt,
       gate.context_summary,
       COALESCE(requester_agent.ref, initiator.ref),
       COALESCE(requester_agent.name, initiator.display_name),
       gate.allowed_decisions,
       gate.state,
       COALESCE(gate.decision, ''),
       gate.decision_comment,
       COALESCE(subject.display_name, ''),
       gate.version,
       gate.created_at,
       gate.resolved_at,
       COALESCE(attachment_set.ref, '')
FROM control_plane.owner_gates gate
LEFT JOIN control_plane.attachment_sets attachment_set
  ON attachment_set.id = gate.resolution_attachment_set_id
JOIN control_plane.projects project ON project.id = gate.project_id
JOIN control_plane.runs root ON root.id = gate.root_run_id
JOIN control_plane.run_nodes node ON node.id = gate.node_id
JOIN control_plane.subjects initiator ON initiator.id = root.initiated_by
LEFT JOIN control_plane.run_nodes requester_node ON requester_node.id = node.parent_node_id
LEFT JOIN control_plane.agents requester_agent ON requester_agent.id = requester_node.agent_id
LEFT JOIN control_plane.subjects subject ON subject.id = gate.resolved_by
WHERE gate.organization_id = $1::uuid
  AND gate.ref = $2
