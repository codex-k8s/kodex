-- name: workflow_catalog__publication :one
SELECT w.ref,p.ref,w.name,w.purpose,COALESCE(a.ref,''),w.state,w.version,w.draft_spec,
       published.spec,published.version_number,w.created_at,w.updated_at,published.ref,
       false,true,published.digest
FROM control_plane.workflows w
JOIN control_plane.projects p ON p.id=w.project_id AND p.organization_id=w.organization_id AND p.lifecycle='ACTIVE'
JOIN control_plane.workflow_versions published ON published.workflow_id=w.id
 AND published.organization_id=w.organization_id AND published.version_number=w.published_version
LEFT JOIN control_plane.agents a ON a.id=w.coordinator_agent_id
WHERE w.organization_id=@organization_id::uuid AND w.project_id=@project_id::uuid
 AND w.ref=@workflow_ref AND w.state='PUBLISHED'
 AND control_plane.catalog_resource_visible(w.organization_id,@actor_id::uuid,'workflow.view','WORKFLOW',
     w.id,w.project_id,w.created_by,jsonb_build_object('PROJECT',w.project_id::text),statement_timestamp())
 AND control_plane.catalog_resource_visible(w.organization_id,@actor_id::uuid,'workflow.launch','WORKFLOW',
     w.id,w.project_id,w.created_by,jsonb_build_object('PROJECT',w.project_id::text),statement_timestamp());
