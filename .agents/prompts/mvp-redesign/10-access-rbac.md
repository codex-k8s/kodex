# Enterprise RBAC и OIDC

## Источники

- `docs/design/mockups/17_access_desktop.dc.html`.
- `services/staff/control-center/src/pages/AccessPage.vue`.
- `docs/domains/identity-access.md`.
- `docs/product/requirements.md`.

## Результат

Создай:

- `access-members-desktop.html`;
- `access-groups-desktop.html`;
- `access-roles-desktop.html`;
- `access-role-editor-desktop.html`;
- `access-effective-desktop.html`;
- `access-agent-scope-desktop.html`;
- `access-mobile.html`.

Keycloak или другой OIDC provider отвечает за identity и группы, а Kodex
control-plane - за прикладные полномочия. Не изображай тысячи project/agent
roles внутри Keycloak.

Раздел использует вкладки «Участники», «Группы OIDC», «Роли» и «Эффективный
доступ». Системные роли видимы и неизменяемы; пользовательские роли можно
создавать и версионировать.

Role binding поддерживает scopes:

- Organization;
- Project;
- тип сущности;
- конкретный ИИ-сотрудник, Workflow или Integration Connection.

Обязательный сценарий: сотруднику-человеку можно разрешить видеть один Проект и
запускать только двух выбранных ИИ-сотрудников в нём. Для этого покажи готовый
preset, async picker сотрудников и итоговый effective access preview.

Для каждого permission покажи русское название, описание действия, риск и
источник назначения: system role, custom role, OIDC group или direct binding.
Экран explain-access отвечает, почему точное действие разрешено или запрещено.

В MVP используются только allow-bindings без explicit deny. UI не редактирует
сырой массив строк permissions и не смешивает platform role с project access.
