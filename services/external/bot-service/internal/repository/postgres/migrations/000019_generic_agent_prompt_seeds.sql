-- +goose Up
insert into matter_codex_agent_profiles(name, role, description)
values
	('manager', 'manager', 'Generic coordination agent profile seed'),
	('architect', 'architect', 'Generic architecture and product documentation agent profile seed'),
	('developer', 'developer', 'Generic implementation and review-fix agent profile seed'),
	('reviewer', 'reviewer', 'Generic pull request reviewer agent profile seed'),
	('docs', 'docs', 'Generic documentation agent profile seed'),
	('sre', 'sre', 'Generic SRE and deployment agent profile seed'),
	('qa-bot', 'qa-bot', 'Generic QA, smoke, and regression agent profile seed'),
	('ui-designer', 'designer', 'Generic UI/UX designer agent profile seed'),
	('improver', 'improver', 'Generic instruction improvement agent profile seed'),
	('pm-delivery', 'pm_delivery', 'Generic project and delivery status agent profile seed'),
	('analyst', 'analyst', 'Generic analysis and requirements agent profile seed'),
	('mattercodex-admin', 'sre', 'Generic owner administration agent profile seed for MatterCodex itself')
on conflict (name) do update
set role = excluded.role,
	description = excluded.description,
	updated_at = now();

-- Prompt bodies are seeded from services/external/bot-service/internal/domain/service/prompt_seeds/*.md
-- by bot-service startup code. Existing database-edited templates are not overwritten.

-- +goose Down
delete from matter_codex_agent_profiles
where name in (
	'architect',
	'docs',
	'sre',
	'qa-bot',
	'ui-designer',
	'improver',
	'pm-delivery',
	'analyst',
	'mattercodex-admin'
);
