-- name: platform__queries_listagents_select_agents_organization_id_ref_project_id :many
SELECT a.ref,p.ref,role.ref,role.name,COALESCE(a.system_key,''),a.name,a.purpose,a.role_description,a.avatar_url,a.state,a.enabled,a.version,
		a.runtime_key,r.name,r.provider,r.model,r.runtime_revision,a.capabilities,
		COALESCE((SELECT array_agg(ar.ref ORDER BY b.created_at) FROM control_plane.artifact_bindings b JOIN control_plane.artifacts ar ON ar.id=b.artifact_id WHERE b.target_kind='KNOWLEDGE' AND b.target_ref=a.ref AND ar.scan_state='CLEAN'),'{}'),
		a.created_at,a.updated_at
		FROM control_plane.agents a LEFT JOIN control_plane.projects p ON p.id=a.project_id JOIN control_plane.role_definitions role ON role.id=a.role_definition_id JOIN control_plane.runtime_profiles r ON r.stable_key=a.runtime_key
		WHERE a.organization_id=$1::uuid AND a.system_key IS NULL AND p.ref=$2 AND a.state<>'ARCHIVED'
		AND ($3 IN ('OWNER','ADMINISTRATOR') OR EXISTS(SELECT 1 FROM control_plane.memberships m WHERE m.project_id=p.id AND m.subject_id=$4::uuid AND m.active AND 'VIEW'=ANY(m.permissions)))
		AND ($5='' OR a.name ILIKE '%'||$5||'%' OR a.purpose ILIKE '%'||$5||'%') AND ($6='' OR a.state=$6)
		ORDER BY a.updated_at DESC LIMIT $7
