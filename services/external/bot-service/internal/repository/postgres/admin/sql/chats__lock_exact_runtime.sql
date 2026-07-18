select project_id, mattermost_channel_id
from matter_codex_chats
where id = $1
for share;
