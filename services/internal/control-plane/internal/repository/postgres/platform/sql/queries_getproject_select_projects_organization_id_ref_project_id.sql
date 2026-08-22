-- name: queries_getproject_select_projects_organization_id_ref_project_id :one
SELECT p.id::text,p.ref,p.name,p.purpose,p.language,p.lifecycle,p.version,p.created_at,p.updated_at,
       (SELECT count(*)::integer FROM control_plane.agents agent WHERE agent.project_id=p.id AND agent.state<>'ARCHIVED'),
       (SELECT count(*)::integer FROM control_plane.workflows workflow WHERE workflow.project_id=p.id AND workflow.state<>'ARCHIVED'),
       (SELECT count(*)::integer FROM control_plane.runs execution WHERE execution.project_id=p.id AND execution.state IN ('QUEUED','RUNNING','WAITING_HUMAN','CANCELLING')),
       (SELECT count(*)::integer FROM control_plane.owner_gates gate WHERE gate.project_id=p.id AND gate.state='OPEN')
		FROM control_plane.projects p WHERE p.organization_id=$1::uuid AND p.ref=$2
		AND ($3 IN ('OWNER','ADMINISTRATOR') OR EXISTS(SELECT 1 FROM control_plane.memberships m WHERE m.project_id=p.id AND m.subject_id=$4::uuid AND m.active AND 'VIEW'=ANY(m.permissions)))
