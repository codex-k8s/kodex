-- name: platform__queries_listworkflows_1 :many
SELECT w.ref,p.ref,w.name,w.purpose,COALESCE(a.ref,''),w.state,w.version,w.draft_spec,w.published_spec,w.published_version,w.created_at,w.updated_at
		FROM control_plane.workflows w JOIN control_plane.projects p ON p.id=w.project_id LEFT JOIN control_plane.agents a ON a.id=w.coordinator_agent_id
		WHERE w.organization_id=$1::uuid AND p.ref=$2 AND w.state<>'ARCHIVED' AND ($3 IN ('OWNER','ADMINISTRATOR') OR EXISTS(SELECT 1 FROM control_plane.memberships m WHERE m.project_id=p.id AND m.subject_id=$4::uuid AND m.active AND 'VIEW'=ANY(m.permissions)))
		AND ($5='' OR w.name ILIKE '%'||$5||'%') AND ($6='' OR w.state=$6) ORDER BY w.updated_at DESC LIMIT $7
