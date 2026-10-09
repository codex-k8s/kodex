-- name: workflow_catalog__active_runs :many
SELECT r.ref,workflow.ref,version.ref,version.digest,version.version_number,r.version,
       r.title,r.state,r.created_at
FROM control_plane.runs r
JOIN control_plane.catalog_access_targets target ON target.kind='RUN' AND target.id=r.id
 AND target.organization_id=r.organization_id
LEFT JOIN control_plane.workflow_versions version ON version.id=r.workflow_version_id
 AND version.organization_id=r.organization_id
LEFT JOIN control_plane.workflows workflow ON workflow.id=version.workflow_id
 AND workflow.organization_id=r.organization_id AND workflow.project_id=r.project_id
 AND workflow.ref=r.target_ref
WHERE r.organization_id=@organization_id::uuid AND r.project_id=@project_id::uuid
 AND r.target_type='WORKFLOW' AND r.id=r.root_run_id AND r.target_ref=@workflow_ref
 AND r.state IN ('QUEUED','RUNNING','WAITING_HUMAN','CANCELLING')
 AND (@cursor='' OR r.ref>@cursor)
 AND control_plane.catalog_resource_visible(target.organization_id,@actor_id::uuid,'run.view',
     target.kind,target.id,target.project_id,target.owner_id,target.related_ids,statement_timestamp())
ORDER BY r.ref LIMIT 11;
