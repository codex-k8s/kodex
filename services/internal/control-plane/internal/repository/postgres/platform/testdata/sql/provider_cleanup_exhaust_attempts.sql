UPDATE control_plane.provider_credential_cleanup_tasks
SET attempts=maximum_attempts WHERE ref=$1 AND state='CLAIMED';
