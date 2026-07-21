select id, digest, manifest::text, account_alias, authorization_revision, created_at
from matter_codex_runtime_revisions
where digest = $1;
