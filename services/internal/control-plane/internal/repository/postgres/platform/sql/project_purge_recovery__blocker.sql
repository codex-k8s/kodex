-- name: project_purge_recovery__blocker :one
SELECT control_plane.artifact_manual_project_pending_count(@organization_id::uuid,@project_id::uuid)>0 OR control_plane.kubernetes_project_unreconciled_count(@organization_id::uuid,@project_id::uuid)>0 OR EXISTS (SELECT 1 FROM control_plane.repository_bundle_publications publication
 WHERE publication.organization_id=@organization_id::uuid AND publication.project_id=@project_id::uuid
   AND (publication.state IN ('WAITING_APPROVAL','QUEUED','CLAIMED','EFFECT_STARTED','UNKNOWN_OUTCOME') OR publication.continuation_state='BLOCKED')) OR EXISTS (
 SELECT 1 FROM control_plane.prepared_content content
 WHERE content.organization_id=@organization_id::uuid AND content.project_id=@project_id::uuid
   AND content.state NOT IN ('ADOPTED','CLEANED')
   AND NOT control_plane.kubernetes_prepared_project_purge_eligible(content.id,@organization_id::uuid,@project_id::uuid)
   AND NOT (content.state='OWNER_ABANDONED' AND content.owner_resolution IS NOT NULL
     AND (content.recovery_state='DELETED_CONFIRMED' OR
       (content.recovery_state='OBSERVING' AND content.recovery_absent_at IS NOT NULL)))
);
