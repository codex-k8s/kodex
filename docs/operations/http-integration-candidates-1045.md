---
id: HTTP-INTEGRATION-CANDIDATES-1045
title: HTTP-каталоги адресных разрешений интеграций
status: approved
owner: manager
version: 1.0.0
updated: 2026-09-06
---

# HTTP-каталоги адресных разрешений интеграций

Источники: Issue #1045, Issue #1046, MVP-UI-39/40 из
`.agents/mvp-finish/11-integration-catalog-and-email.md`.
Четыре типизированных запроса используют signed actor/organization и защищённый
PlatformQueryService. Параметры выбирают контекст чтения, но не назначают полномочия.
Все четыре client profile используют UNARY_PROTO_SHA256. Metadata resource,
version, attempt и idempotency запрещены; project_required=false. Context входит
в полный digest запроса, а заданный trusted principal project дополнительно
сужает owner eligibility. Gateway не создаёт resource headers из query.

| Инициатор и внешний GET | RPC владельца CP | Контекст и результат |
| --- | --- | --- |
| Выдача разрешения: `/api/v1/integration-grant-candidates/connections?purpose=GRANT` | ListIntegrationGrantConnectionCandidates | Пустой контекст; возможность выдачи, версия connection и точный package |
| Выбор Project: `/api/v1/integration-grant-candidates/projects` | ListIntegrationGrantProjectCandidates | connectionRef; доступные Projects и их версии |
| Выбор адресата: `/api/v1/integration-grant-candidates/recipients` | ListIntegrationGrantRecipientCandidates | connectionRef + projectRef + recipientKind; выбранный тип AGENT/WORKFLOW фильтруется до count/page |
| Выбор возможности: `/api/v1/integration-grant-candidates/capabilities` | ListIntegrationGrantCapabilityCandidates | Предыдущий префикс + recipientKind/ref; actual package capability, текущий grant ref/version |
| Использование: первый endpoint с `purpose=USE` | ListIntegrationGrantConnectionCandidates | Exact Project + Agent + capabilityKey, optional парные workflowRef/stepKey; только действующее пересечение полномочий |

Каждый запрос сохраняет literal query, pageSize 1–100, opaque pageToken до 2048.
Owner применяет одну eligibility к странице и total. Response содержит exact echo
контекста, server contextDigest и pins выбранного префикса. Item pins добавляют
выбранный элемент; общий префикс не может поменять версии внутри ответа.
DefinitionVersion остаётся строкой. Workflow revision фиксируется только для USE
с workflowRef/stepKey; GRANT Workflow использует recipientVersion.

GRANT READY означает grantable=true/usable=false, USE READY — обратное.
USE не превращается в выдачу разрешения и не допускает самостоятельного Workflow
вместо Agent. Неполные/смешанные префиксы, неизвестные и повторные query-поля
отклоняются до RPC. Неизвестные reasons, противоречивые booleans, чужой echo,
неполные pins и небезопасные числовые версии дают INVALID_UPSTREAM_RESPONSE.

ResourceScope содержит только публичные actual package поля: до 8 ключей,
ключ до 120, значение до 349528 символов. В GRANT scope пуст, credentialKind
отсутствует либо TOKEN/PASSWORD. Credential bytes и внутренние grants не выдаются.

Это read-only операции: If-Match/idempotency не используются, состояние и событие
не создаются. Повторное чтение служит авторитетным read path. Создание/изменение
grant остаётся отдельной owner-командой с актуальной проверкой полномочий до OCC
и receipt replay. SDK/PWA должны последовательно сбрасывать зависимые выборы
при изменении префикса, не объединять локальные страницы в собственный каталог.

## Проверка

Targeted gateway tests проверяют реальные generated routes → typed RPC,
GRANT/USE, literal query, пустой total, запрет смешанных префиксов до RPC и
отказ при подмене context/pins/reason. Общий gateway baseline и contract replay
фиксируются на итоговом SHA в PR #1066. Live/PWA acceptance этим документом
не объявляется выполненным.
