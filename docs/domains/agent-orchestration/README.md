# Оркестрация агентов

## Назначение

Домен описывает `agent-manager`: flow, этапы, роли, шаблоны промптов, агентные сессии, `Run`, машину приёмки, follow-up задачи и запуск ролевых агентов через runtime-контур.

## Что входит

- `agent-manager` как сервис-владелец оркестрации агентов;
- flow, stage, role, stage-role binding и prompt template;
- версии flow, ролей и prompt, используемые в конкретном `Run`;
- агентные сессии и агентные запуски;
- машина приёмки;
- правила создания follow-up provider-native задач;
- запуск ролевых агентов через `runtime-manager`;
- чтение руководящих пакетов через `package-hub`;
- provider-native операции через `provider-hub`;
- Human gate и обратная связь через `interaction-hub`;
- MCP-инструменты через `platform-mcp-server`.

## Что не входит

- Слоты, workspace filesystem и platform jobs принадлежат `runtime-manager`.
- Provider-native `Issue`, `PR/MR`, комментарии, связи и GitHub/GitLab операции принадлежат `provider-hub`.
- Пакеты, версии, manifest и установки принадлежат `package-hub`.
- Диалоги, уведомления, внешние каналы и решения человека принадлежат `interaction-hub`.
- Проектная политика, `services.yaml` и workspace policy принадлежат `project-catalog`.

## Документы

| Документ | Путь |
|---|---|
| Требования | `product/requirements.md` |
| Дизайн | `architecture/design.md` |
| Модель данных | `architecture/data_model.md` |
| API-обзор | `architecture/api_contract.md` |
| План поставки | `delivery/agent_manager_delivery.md` |

## Связанные каталоги

- `docs/catalogs/prompt-roles/`.
- `docs/catalogs/guidance-packages/`.

## Карта Issue

- Доменная карта: `docs/delivery/issue-map/domains/agent-orchestration.md`.
