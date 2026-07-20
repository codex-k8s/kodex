select project_id, enabled
from matter_codex_agent_roles
where id = $1
for share;
