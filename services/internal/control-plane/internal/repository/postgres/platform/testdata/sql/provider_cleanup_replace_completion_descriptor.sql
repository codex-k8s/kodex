UPDATE control_plane.provider_credential_cleanup_tasks
SET completion_descriptor=$2::jsonb WHERE ref=$1;
