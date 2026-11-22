-- name: project_purge_recovery__snapshot :one
WITH pending AS MATERIALIZED (
 SELECT project.id,project.ref,project.version,
  control_plane.kubernetes_project_unreconciled_count(project.organization_id,project.id) staging,
  control_plane.artifact_manual_project_pending_count(project.organization_id,project.id) manual
 FROM control_plane.projects project
 WHERE project.organization_id=@organization_id::uuid AND project.ref=@project_ref
), blocked AS (
 SELECT id,ref,version,CASE WHEN manual>0 THEN manual ELSE staging END pending,
  CASE WHEN manual>0 THEN 'ARTIFACT_PURGE_RECONCILIATION_REQUIRED' ELSE 'KUBERNETES_STAGING_RECONCILIATION_REQUIRED' END reason
 FROM pending
)
SELECT coalesce((SELECT jsonb_build_object('projectRef',ref,'version',version,
 'snapshotDigest',encode(public.digest(convert_to(jsonb_build_object('v',1,'project',ref,'version',version,
   'reason',reason,'count',pending)::text,'UTF8'),'sha256'),'hex'),
 'state','BLOCKED','unknownContentCount',least(pending,2147483647),'cleanupPendingCount',0,
 'projectPresent',true,'projectVersion',version,'canResolve',false,'resolutionRef','',
 'reasonCode',reason) FROM blocked WHERE pending>0),
 control_plane.project_purge_recovery_snapshot(@organization_id::uuid,@project_ref,@storage_identity));
