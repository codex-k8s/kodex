-- +goose Up
-- Keep persisted role and catalog prompts aligned with runtime routing rules.
-- Same-thread delegation is valid only for an enabled chat participant; account
-- aliases and logins are never admission criteria for GitHub operations.
update matter_codex_agent_roles
set prompt_template = replace(
	replace(
		replace(
			prompt_template,
			$$- Для запуска другого агента в текущем треде используй `mattermost_request_agent(target_agent, message)`.$$,
			$$- Для запуска другого агента в текущем треде используй `mattermost_request_agent(target_agent, message)` только если карточка текущего чата содержит эту роль среди включенных агентов. Иначе такой запуск будет отклонен: выбери подходящий чат через каталог и создай отдельный дочерний тред.$$
		),
		$$- Запускай других агентов только через `mattermost_request_agent`.$$,
		$$- Запускай другого агента через `mattermost_request_agent` только если он указан включенным участником текущего чата. Для межчатовой работы проверь каталог через `mattermost_list_chats` и `mattermost_get_chat`, затем используй `mattermost_start_agent_thread`.$$
	),
	$$- В дочернем промпте передавай требуемое действие и уровень доступа. Не указывай и не требуй конкретный GitHub account alias, login или identity, не копируй identity координатора и не делай имя владельца условием выполнения; целевой агент проверит фактическую возможность операции.$$,
	$$- В дочернем промпте передавай требуемое действие и уровень доступа. Не указывай и не требуй конкретный GitHub account alias, login или identity, не копируй identity координатора и не делай имя владельца условием выполнения; целевой агент проверит фактическую возможность операции. Если такое требование осталось в старом треде, callback или задаче, не повторяй его.$$
),
	updated_at = now()
where prompt_template is not null
	and prompt_template ~ '(Для запуска другого агента в текущем треде используй|Запускай других агентов только через|В дочернем промпте передавай требуемое действие)';

update matter_codex_agent_prompt_templates
set body = replace(
	replace(
		replace(
			body,
			$$- Для запуска другого агента в текущем треде используй `mattermost_request_agent(target_agent, message)`.$$,
			$$- Для запуска другого агента в текущем треде используй `mattermost_request_agent(target_agent, message)` только если карточка текущего чата содержит эту роль среди включенных агентов. Иначе такой запуск будет отклонен: выбери подходящий чат через каталог и создай отдельный дочерний тред.$$
		),
		$$- Запускай других агентов только через `mattermost_request_agent`.$$,
		$$- Запускай другого агента через `mattermost_request_agent` только если он указан включенным участником текущего чата. Для межчатовой работы проверь каталог через `mattermost_list_chats` и `mattermost_get_chat`, затем используй `mattermost_start_agent_thread`.$$
	),
	$$- В дочернем промпте передавай требуемое действие и уровень доступа. Не указывай и не требуй конкретный GitHub account alias, login или identity, не копируй identity координатора и не делай имя владельца условием выполнения; целевой агент проверит фактическую возможность операции.$$,
	$$- В дочернем промпте передавай требуемое действие и уровень доступа. Не указывай и не требуй конкретный GitHub account alias, login или identity, не копируй identity координатора и не делай имя владельца условием выполнения; целевой агент проверит фактическую возможность операции. Если такое требование осталось в старом треде, callback или задаче, не повторяй его.$$
),
	updated_at = now()
where body ~ '(Для запуска другого агента в текущем треде используй|Запускай других агентов только через|В дочернем промпте передавай требуемое действие)';

-- +goose Down
-- +goose StatementBegin
do $$
begin
	raise exception 'migration 000034 is forward-only: prompt routing guidance cannot be restored safely';
end
$$;
-- +goose StatementEnd
