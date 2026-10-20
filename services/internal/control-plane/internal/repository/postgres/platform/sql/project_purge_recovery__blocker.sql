-- name: project_purge_recovery__blocker :one
SELECT EXISTS (
 SELECT 1 FROM control_plane.prepared_content content
 WHERE content.organization_id=@organization_id::uuid AND content.project_id=@project_id::uuid
   AND content.state NOT IN ('ADOPTED','CLEANED')
   AND NOT (content.state='OWNER_ABANDONED' AND content.owner_resolution IS NOT NULL
     AND (content.recovery_state='DELETED_CONFIRMED' OR
       (content.recovery_state='OBSERVING' AND content.recovery_absent_at IS NOT NULL)))
);
