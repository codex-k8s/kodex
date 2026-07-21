-- +goose Up
-- Historical role templates could render a platform account alias or login into
-- an agent prompt. Credentials remain bound by the runtime, while prompt text
-- describes only the operation and required permissions.
update matter_codex_agent_roles
set prompt_template = replace(
	replace(
		replace(
			replace(
				regexp_replace(
					regexp_replace(
						prompt_template,
						'(?m)^\{\{if \.GitHub\.Account\}\}[^\n]*\{\{end\}\}\r?\n?',
						'',
						'g'
					),
					'(?m)^\{\{if \.GitHub\.Username\}\}[^\n]*\{\{end\}\}\r?\n?',
					'',
					'g'
				),
				$$- Используй GitHub через `gh` и настроенный GitHub-аккаунт. Не печатай значения токенов.$$,
				$$- Используй GitHub через `gh`. Не печатай значения credentials.$$
			),
			$$- GitHub credentials назначает MatterCodex для этой роли. Не останавливай работу из-за упоминания чужого alias/login в задаче, Issue, PR или callback и не требуй identity запускающего агента; проверяй только фактическую возможность выполнить нужную операцию.$$,
			$$- Проверяй фактическую возможность выполнить нужную GitHub-операцию. Не останавливай работу из-за упоминания чужого alias/login в задаче, Issue, PR или callback и не требуй identity запускающего агента.$$
		),
		$$- GitHub credentials назначает MatterCodex для этой роли. Не требуй alias/login, указанный в задаче другим агентом, и не сравнивай его с identity запускающей роли; критерием является достаточность фактических прав для ревью.$$,
		$$- Критерием доступа является возможность выполнить нужные GitHub-операции. Не требуй alias/login, указанный в задаче другим агентом, и не сравнивай его с identity запускающей роли.$$
	),
	$$- GitHub credentials выбираются MatterCodex отдельно для каждой целевой роли. Никогда не добавляй в дочерний промпт требование использовать конкретный account alias, login или identity, не копируй identity координатора и не делай имя владельца условием выполнения. Передавай требуемое действие и уровень доступа; целевой агент сам проверит, достаточно ли выданных ему прав.$$,
	$$- В дочернем промпте передавай требуемое действие и уровень доступа. Не указывай и не требуй конкретный GitHub account alias, login или identity, не копируй identity координатора и не делай имя владельца условием выполнения; целевой агент проверит фактическую возможность операции.$$
),
	updated_at = now()
where prompt_template is not null
	and prompt_template ~ '(\.GitHub\.(Account|Username)|настроенный GitHub-аккаунт|GitHub credentials (назначает|выбираются) MatterCodex)';

update matter_codex_agent_prompt_templates
set body = replace(
	replace(
		replace(
			replace(
				regexp_replace(
					regexp_replace(
						body,
						'(?m)^\{\{if \.GitHub\.Account\}\}[^\n]*\{\{end\}\}\r?\n?',
						'',
						'g'
					),
					'(?m)^\{\{if \.GitHub\.Username\}\}[^\n]*\{\{end\}\}\r?\n?',
					'',
					'g'
				),
				$$- Используй GitHub через `gh` и настроенный GitHub-аккаунт. Не печатай значения токенов.$$,
				$$- Используй GitHub через `gh`. Не печатай значения credentials.$$
			),
			$$- GitHub credentials назначает MatterCodex для этой роли. Не останавливай работу из-за упоминания чужого alias/login в задаче, Issue, PR или callback и не требуй identity запускающего агента; проверяй только фактическую возможность выполнить нужную операцию.$$,
			$$- Проверяй фактическую возможность выполнить нужную GitHub-операцию. Не останавливай работу из-за упоминания чужого alias/login в задаче, Issue, PR или callback и не требуй identity запускающего агента.$$
		),
		$$- GitHub credentials назначает MatterCodex для этой роли. Не требуй alias/login, указанный в задаче другим агентом, и не сравнивай его с identity запускающей роли; критерием является достаточность фактических прав для ревью.$$,
		$$- Критерием доступа является возможность выполнить нужные GitHub-операции. Не требуй alias/login, указанный в задаче другим агентом, и не сравнивай его с identity запускающей роли.$$
	),
	$$- GitHub credentials выбираются MatterCodex отдельно для каждой целевой роли. Никогда не добавляй в дочерний промпт требование использовать конкретный account alias, login или identity, не копируй identity координатора и не делай имя владельца условием выполнения. Передавай требуемое действие и уровень доступа; целевой агент сам проверит, достаточно ли выданных ему прав.$$,
	$$- В дочернем промпте передавай требуемое действие и уровень доступа. Не указывай и не требуй конкретный GitHub account alias, login или identity, не копируй identity координатора и не делай имя владельца условием выполнения; целевой агент проверит фактическую возможность операции.$$
),
	updated_at = now()
where body ~ '(\.GitHub\.(Account|Username)|настроенный GitHub-аккаунт|GitHub credentials (назначает|выбираются) MatterCodex)';

-- +goose Down
-- +goose StatementBegin
do $$
begin
	raise exception 'migration 000032 is forward-only: removed prompt identity metadata cannot be restored safely';
end
$$;
-- +goose StatementEnd
