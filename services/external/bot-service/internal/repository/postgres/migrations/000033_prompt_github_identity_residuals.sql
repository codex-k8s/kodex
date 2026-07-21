-- +goose Up
-- Remove residual custom and legacy lines that expose GitHub account aliases or
-- logins. Environment-name placeholders remain available for git/gh setup.
update matter_codex_agent_roles
set prompt_template = regexp_replace(
	prompt_template,
	'(?m)^.*\.GitHub\.(Account|Username)([^A-Za-z0-9_]|$).*\r?\n?',
	'',
	'g'
),
	updated_at = now()
where prompt_template is not null
	and prompt_template ~ '\.GitHub\.(Account|Username)([^A-Za-z0-9_]|$)';

update matter_codex_agent_prompt_templates
set body = regexp_replace(
	body,
	'(?m)^.*\.GitHub\.(Account|Username)([^A-Za-z0-9_]|$).*\r?\n?',
	'',
	'g'
),
	updated_at = now()
where body ~ '\.GitHub\.(Account|Username)([^A-Za-z0-9_]|$)';

-- +goose Down
-- +goose StatementBegin
do $$
begin
	raise exception 'migration 000033 is forward-only: removed prompt identity lines cannot be restored safely';
end
$$;
-- +goose StatementEnd
