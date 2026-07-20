select enabled
from matter_codex_chat_participants
where chat_id = $1 and role_id = $2
for share;
