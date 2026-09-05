-- name: secret_draft_readiness :one
SELECT has_table_privilege(current_user,'control_plane.runtime_secret_drafts','SELECT, INSERT, UPDATE')
   AND has_table_privilege(current_user,'control_plane.runtime_secret_draft_operations','SELECT, INSERT, UPDATE')
   AND has_table_privilege(current_user,'control_plane.runtime_secret_draft_impact_plans','SELECT, INSERT, UPDATE')
   AND has_table_privilege(current_user,'control_plane.runtime_secret_draft_impact_items','SELECT, INSERT, UPDATE')
   AND has_sequence_privilege(current_user,'control_plane.runtime_secret_drafts_generation_seq','USAGE, SELECT')
   AND EXISTS(SELECT 1 FROM pg_catalog.pg_indexes WHERE schemaname='control_plane' AND indexname='runtime_secret_draft_operation_active');
