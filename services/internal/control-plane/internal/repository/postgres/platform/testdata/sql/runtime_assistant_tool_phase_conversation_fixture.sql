UPDATE control_plane.assistant_conversations
SET state='ARCHIVED',deleted_at=clock_timestamp(),purge_after=clock_timestamp()+interval '30 days'
WHERE ref=$1;
