---
doc_id: ARC-CK8S-PROVIDER-INTEGRATION-0001
type: api-contract
title: kodex — модель интеграции с провайдерами
status: active
owner_role: SA
created_at: 2026-04-26
updated_at: 2026-04-26
related_issues: [599, 600, 601, 602]
related_prs: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-04-26-platform-architecture-frame"
  approved_by: "ai-da-stas"
  approved_at: 2026-04-26
---

# Модель интеграции с провайдерами

## TL;DR

GitHub является первой реализацией provider-контура, GitLab должен лечь в ту же архитектурную границу. Slot-агенты могут работать с provider-native артефактами через `gh` или нативный API, а быстрый agent-manager и платформенные операции идут через `platform-mcp-server` и `provider-hub`. UI и acceptance работают по зеркальным проекциям и не читают провайдера на каждый экран.

## Provider-контур

Provider-контур покрывает:
- репозитории кода и рабочих артефактов;
- репозитории проектной документации;
- репозитории пакетов и руководящей документации;
- release branches и tags;
- внешние аккаунты, лимиты, авторизацию и блокировки;
- будущие внешние системы, где платформа не хранит истину сама.

`provider-hub` является owner-сервисом provider-интеграции. Он изолирует особенности GitHub/GitLab и отдаёт остальным сервисам нормализованные контракты.

## Категории операций

| Категория | Кто вызывает | Как проходит |
|---|---|---|
| Webhook event | Провайдер | `api-gateway` валидирует edge, `provider-hub` сохраняет inbox и нормализует событие. |
| Canonical read | Owner-сервис или acceptance | `provider-hub` читает провайдера с учётом лимитов и account policy. |
| Provider operation | Agent-manager или platform service | Через `platform-mcp-server` и `provider-hub`, если операция платформенная или требует policy/audit. |
| Slot-agent work | Ролевой агент | Через `gh`, `glab` или нативный API в slot по профилю роли. |
| Reconciliation | `worker` по поручению `provider-hub` | Incremental cursor, окно перекрытия, hot/warm/cold приоритеты. |

## Нормализованные объекты

| Объект | Минимальный смысл |
|---|---|
| `ProviderAccount` | Аккаунт, класс, область действия, статус авторизации, лимиты и доступные операции. |
| `ProviderRepository` | Внешний репозиторий, owner, visibility, provider id, URL и project binding. |
| `ProviderWorkItem` | Нормализованный `Issue` или `PR/MR` с provider id, номером, состоянием, заголовком, labels, assignees, milestone и project fields. |
| `ProviderComment` | Комментарий, mention или review signal, связанный с work item. |
| `ProviderRelationship` | Связь между `Issue`, `PR/MR`, follow-up, блокировкой, релизом или артефактом. |
| `ProviderLimit` | Текущий known state лимитов, reset time, класс ограничения и affected account. |
| `ProviderOperation` | Запись внешней операции, actor, account, target, результат и ошибка. |

## Синхронизация проекций

### Webhook inbox

Webhook является быстрым входом, но не единственной гарантией актуальности.

Обязательные правила:
- входящее событие сначала сохраняется в inbox;
- dedup выполняется по delivery id или provider-аналогу;
- нормализация и пересчёт проекции идут асинхронно;
- ошибка обработки не теряет исходный payload до истечения retention;
- каждый пересчёт публикует доменное событие для потребителей.

### Incremental reconciliation

Reconciliation обязателен, потому что webhook может потеряться или прийти с задержкой.

Правила:
- хранить `sync_cursor` по области синхронизации: repo, artifact type или другой provider scope;
- использовать окно перекрытия для пограничных изменений;
- различать hot, warm и cold entities;
- при дефиците лимитов сохранять бюджет на горячие сущности;
- повышать `drift_status`, если проекция могла устареть.

### Сигнал от slot-агента

Если slot-агент создал или изменил артефакт через `gh` или API, он передаёт платформе provider id, URL или owner/repo/number как ускоряющий сигнал. Этот сигнал не заменяет webhook и reconciliation, а только помогает быстрее обновить проекцию.

## Лимиты и учёт `gh`

Операции через `provider-hub` учитываются детерминированно: платформа знает account, operation, target, response headers и итог.

Для прямой работы slot-агента через `gh` действует приближённый учёт:
- до и после agent-run снимается `gh api rate_limit`;
- для высокообъёмных действий агент отправляет промежуточные снимки через MCP;
- если используется `gh api`, агент может передать response headers, полученные через `gh api --include`;
- высокоуровневые команды `gh issue` и `gh pr` не считаются полным источником телеметрии.

Целевое усиление: операции, где нужен строгий лимитный бюджет, должны идти через provider proxy или MCP-инструменты платформы.

## Provider-native поля

Платформа использует provider-native поля там, где они полезны человеку и не требуют постоянного переписывания быстро меняющегося runtime state.

Допустимые поля:
- type;
- labels;
- relationships;
- comments и mentions;
- state;
- review state;
- assignees;
- milestone;
- project fields;
- branches и tags для release policy.

Не размещать в provider-native полях:
- текущее состояние slot;
- внутренние retry-счётчики;
- полный runtime log;
- технические job details;
- быстро меняющиеся диагностические данные.

## GitHub first, GitLab next

Первая реализация:
- GitHub webhook;
- `Issue`, `PR`, comments, mentions и review;
- GitHub bot/user accounts;
- `gh` в slot;
- rate limit handling;
- provider mirror и reconciliation.

Архитектурные требования для следующего провайдера:
- в доменах нет прямых GitHub REST/GraphQL путей;
- нормализованная модель не содержит GitHub-only обязательных полей без fallback;
- `provider-hub` скрывает различия API;
- UI использует нормализованные проекции.

## Апрув

- request_id: `owner-2026-04-26-platform-architecture-frame`
- Решение: approved
- Комментарий: модель provider-интеграции входит в сквозной архитектурный каркас платформы.
