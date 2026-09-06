-- name: queries_listworkflows_select_workflows_organization_id_ref_project_id :many
SELECT w.ref,p.ref,w.name,w.purpose,COALESCE(a.ref,''),w.state,w.version,w.draft_spec,w.published_spec,w.published_version,w.created_at,w.updated_at,COALESCE(published.ref,''),
		($3 IN ('OWNER','ADMINISTRATOR') OR EXISTS(SELECT 1 FROM control_plane.memberships m WHERE m.project_id=p.id AND m.subject_id=$4::uuid AND m.active AND 'MANAGE_WORKFLOWS'=ANY(m.permissions))),
		($3 IN ('OWNER','ADMINISTRATOR') OR EXISTS(SELECT 1 FROM control_plane.memberships m WHERE m.project_id=p.id AND m.subject_id=$4::uuid AND m.active AND 'LAUNCH_RUNS'=ANY(m.permissions)))
		FROM control_plane.workflows w JOIN control_plane.projects p ON p.id=w.project_id LEFT JOIN control_plane.agents a ON a.id=w.coordinator_agent_id
		LEFT JOIN control_plane.workflow_versions published ON published.workflow_id=w.id
		  AND published.organization_id=w.organization_id AND published.version_number=w.published_version
		WHERE w.organization_id=$1::uuid AND ($2='' OR p.ref=$2) AND w.state<>'ARCHIVED'
		AND ($5='' OR w.name ILIKE '%'||$5||'%') AND ($6='' OR w.state=$6)
		AND ($8='' OR w.ref > $8)
		AND ($9='' OR w.project_id = NULLIF($9,'')::uuid)
		AND control_plane.catalog_resource_visible(w.organization_id, $4::uuid, 'workflow.view', 'WORKFLOW',
		    w.id, w.project_id, w.created_by, jsonb_build_object('PROJECT',w.project_id::text), statement_timestamp())
		ORDER BY w.ref LIMIT $7
