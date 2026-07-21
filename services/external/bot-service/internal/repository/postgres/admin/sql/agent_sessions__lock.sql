select id
from matter_codex_agent_sessions
where session_key = $1
for update;
