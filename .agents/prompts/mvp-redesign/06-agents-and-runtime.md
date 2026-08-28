# ИИ-сотрудники, аватары, инструкции и runtime

## Источники

- `docs/design/mockups/06_agents_desktop.dc.html`.
- `docs/design/mockups/07_agent_detail_desktop.dc.html`.
- `services/staff/control-center/src/pages/AgentsPage.vue`.
- `services/staff/control-center/src/pages/AgentDetailPage.vue`.
- `docs/domains/providers-accounts.md`.
- `docs/architecture/runtime-and-sessions.md`.

## Результат

Создай:

- `agents-grid-desktop.html`;
- `agents-table-desktop.html`;
- `agent-profile-desktop.html`;
- `agent-instructions-desktop.html`;
- `agent-runtime-desktop.html`;
- `agent-environment-picker-desktop.html`;
- `agents-mobile.html`.

Список сотрудников по умолчанию является адаптивной сеткой карточек. Карточка
содержит аватар, имя, назначение, роль, состояние, model/runtime и текущую
активность. Пользователь может переключиться на плотную таблицу; выбор вида
сохраняется.

Kodex должен уметь предложить сотруднику аватар. В макете создания и изменения
покажи:

- действие «Создать аватар с Kodex»;
- безопасный preview;
- «Применить», «Создать другой», «Загрузить свой» и «Удалить»;
- явное указание, что аватар не меняет полномочия и инструкции;
- fallback с инициалами, если изображение отсутствует.

Карточка сотрудника использует вкладки «Профиль», «Инструкции», «Runtime»,
«Окружение», «Доступы и знания».

Инструкции редактируются в CodeMirror-подобном Markdown editor: syntax
highlight, поиск, ошибки, preview, история immutable revisions и возврат версии.
Рядом доступен каталог разрешённых `{{variables}}` с именем, типом, описанием,
примером и autocomplete. Никаких исполняемых произвольных шаблонов.

Runtime показывает раздельно provider, account pool/policy, model, runtime,
environment и versioned `config.toml` overlay. Overlay редактируется как TOML,
валидируется и не позволяет отключить enforced security/platform policy.
Покажи итоговый effective config без secret values.

Рабочее окружение выбирается через async picker: иконка, назначение, tools,
revision, image/build state и совместимость. Он рассчитан на десятки и сотни
элементов, а не на длинный список radio-карточек.
