select session_id, status
from matter_codex_agent_session_turns
where id = $1
for update;
