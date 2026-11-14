---
id: OPS-DOC-SENTRY-INTEGRATIONS-2140
title: Управляемая интеграция Sentry Cloud и Self-hosted
type: operations
status: approved
owner: backend
version: 1.0.0
updated: 2026-11-14
---

# Sentry через существующий integration-gateway

Источники: [#2140](https://github.com/codex-k8s/kodex/issues/2140),
приёмка [#1830](https://github.com/codex-k8s/kodex/issues/1830), личные
credentials [#2033](https://github.com/codex-k8s/kodex/issues/2033),
`ARCH-MC-005`, `GUIDE-DOC-003`, `GUIDE-DOC-006` и `GOV-DOC-003`.
Это описание контракта и локального кандидата, а не подтверждение установки.
Declared compatibility: Sentry Self-hosted26.10.1 и проверенные14.11.2026
Cloud latest API profiles. Upstream release version не выводится из успешного
health: observed compatibility относится только к реально проверенным operations.
Живые Cloud/self-hosted версии и операции пока не наблюдались.

## Граница текущего кандидата

Кандидат содержит две явно разные области: `sentry` /
`sentry-self-hosted` дают13операций выбранного проекта; отдельные
`sentry-organization` / `sentry-self-hosted-organization` дают13операций всей
организации. Выбор нового типа подключения в Control Center и отдельный grant
явно включает полномочия организации. Прежнее project connection, его grant,
configuration и personal proof не расширяются автоматически.

Проектная цепочка: health, issues list/search/detail, event detail/list,
comments list/create, assignment/status, project releases list/read и alerts
list/read. Запись общих releases либо workflows из project connection закрыта:
`UNSUPPORTED_CROSS_PROJECT_RELEASE_MUTATION` и
`UNSUPPORTED_PROJECT_ALERT_ATOMIC_SCOPE`. Эти причины описывают отсутствующие
capabilities, а не новый исполняемый HTTP interface.

Организационная цепочка: organization health, project discovery, общие
releases list/read/create/update metadata, monitors list/read и alerts
list/read/create/update/delete. Release metadata ограничены version, выбранным
project association при create, status и ref; commits, file/artifact uploads и
произвольные URLs не принимаются. Workflow configuration ограничена именем,
включением, одним текущим monitor, тремя фиксированными issue triggers,
email recipient `user:<id>` / `team:<id>` / `issue_owners` и частотой.
Fallback recipient закреплён в `NoOne`; webhooks, plugin actions, произвольный
JSON, organization administration и неизвестные action/condition не допускаются.

Это конечный набор Sentry contract выбранного unit, а не полный Sentry API.
Наблюдаемость реализована в тех же Definition/Connection/Grant и gateway;
границы и квалификация описаны в [операционном контракте](observability-integrations-2140.md).
Локальный candidate и отсутствие live credentials сами по себе не закрывают #2140.

Новый движок, универсальный HTTP tool и shell с токеном не создаются.
Существующие `Definition/Connection/Capability/Grant`, личный root initiator,
одноразовый `BeginPersonalIntegrationInvocation`, согласования, owner leases,
audit и completion остаются единственным путём исполнения. Generic Control
Center строит поля, список capabilities, ввод личного ключа и выбор политики
из переданной Definition; отдельная provider-форма не нужна. Неисполняемые
optional `label/help` дают русские подписи и пояснения, а owner projection
сохраняет реальные INTEGER/BOOLEAN types. Отсутствующие metadata пропускаются
при canonical marshal, поэтому дайджесты прежних definitions не меняются.

## Настройка и доставка

В Connection сохраняются только `base_url`, `organization_slug`, `project_slug`.
Cloud допускает точные `https://sentry.io`, `https://us.sentry.io`,
`https://de.sentry.io`; регион выбирает владелец подключения.
Self-hosted origin использует точный HTTPS hostname443 без path, userinfo,
query, fragment или IP literal. Сегменты организации/проекта ограничены
буквами, цифрами, `_` и `-`; project resource scope выводится сервером из всех трёх
полей. У отдельных organization definitions поле `project_slug` отсутствует;
resource scope содержит origin и organization slug. Изменение любого поля инвалидирует прежние credential/config pins.

Токен хранится под именем ключа `api_token`, значение не возвращается в UI,
RuntimeRevision, prompt, логи, receipts либо документы. Для
`INITIATOR_REQUIRED` отсутствующий или отозванный личный ключ не подменяется
служебным. `PROJECT_SHARED` выбирается только явной политикой владельца.
Sentry DSN не является API token и этой интеграцией не применяется.

Cloud использует существующий отдельный managed CONNECT listener
`egress-gateway-openapi:8083`: CP выводит hostname только из текущего enabled
exact Definition/Connection или полного current published managed binding,
затем обычный publisher проверяет DNS, точную CNI policy, поколение Service и
Deployment. Невалидный managed binding не вызывает shipped fallback.
Self-hosted использует обязательный operator-pinned native listener
`egress-gateway-native:8085`; отсутствие client/policy закрывает вызов.
Connection/prompt не добавляют native pins. Настройка штатного optional
профиля описана в [#2040](native-integration-egress-2040.md).

Оба маршрута сохраняют end-to-end TLS1.3, проверку CA/hostname и запрет
redirects. Нет direct/core transport fallback. Readiness процесса gateway
проверяет служебные зависимости; успешный Connection test дополнительно
читает точный проект через тот же provider client. Это не доказательство
прав на все ошибки проекта и не квалификация живого Sentry. Новый deployable,
listener, Service, workload identity либо Pod secret mount не добавляются;
Docker, метрики, alerts, bounded lifecycle и shutdown наследуются от gateway.

## Сквозная карта контрактов

Actor — проверенный пользователь из owner root state; delegation, retry и
continuation не назначают нового владельца ключа. Browser использует прежние
connection/credential/grant команды `control-api-gateway`; сотрудник — прежний
session-bound MCP. Внутренние RPC claim/Begin/Complete — существующие CP RPC,
новые значения перечислений13..17 несут mandatory consumer profile, а не authority.

| Сценарий | Фиксированный provider path | Проверка и результат |
|---|---|---|
| Project health/read | `GET /api/0/projects/{org}/{project}/` | Ответ содержит exact org/project slug и canonical project ID; выдаются лишь ID, slug и name |
| Issue list/search | `GET /api/0/projects/{org}/{project}/issues/` | До50 items, query до1000 bytes, cursor до128; каждый ответ проверяет exact project ID/slug, дубликаты запрещены |
| Issue detail | `GET /api/0/organizations/{org}/issues/{id}/` | ID из input не доказывает владение: current project GET и returned issue project должны совпасть |
| Event detail | `GET /api/0/projects/{org}/{project}/events/{event}/` | Exact32 lower-hex event ID и projectID; выдаются только ID/title/platform, без request headers, user и breadcrumbs |
| Status update | Current project GET → issue GET → token metadata GET → один `PUT /api/0/organizations/{org}/issues/{id}/` | Current `auth.scopes` содержит `event:write` либо `event:admin`, issue принадлежит project; body содержит только разрешённый status |
| Comments list/create и assignment | Точный issue GET → `/comments/` либо issue PUT | Project boundary проверяется до effect; comment POST дополнительно требует current user-token, совпадение returned author ID и текста; assignment принимает только numeric user/team либо unassigned |
| Project release list/read | Project-scoped `/releases/` | Текущая project membership; numeric/string project IDs сохраняют точность, metadata соседних проектов не выдаются |
| Project alert list/read | Organization `/workflows/` + отдельный `/project-scope/` | Один eligibility predicate скрывает foreign/shared/all-project workflow во всех project paths; количество/имена скрытых ресурсов не выдаются |
| Organization health/discovery | Organization metadata/projects | Только отдельный full-organization Definition/Connection/Grant; project connection не проходит этот профиль |
| Organization release metadata | Org-scoped release GET/POST/PUT | Current token scopes и org-bound project association, безопасный полный affected-project набор из provider state; один effect, shared semantics явно разрешены full-org grant |
| Organization alert CRUD | Org-scoped workflow, monitor и project-scope | Current provider scopes, принадлежность org, finite typed email profile; success сверяет точные trigger/recipient/frequency; DELETE204 означает deletion requested |
| Personal key test | Прежний health + bounded inputless READ + `GET /api/0/` | Health/READ observations отдельно от `SENTRY_TOKEN_SCOPES_V1`; известный недостаточный scope даёт точный deny для каждой operation, неизвестные metadata оставляют запись неподтверждённой |

Чтения имеют `READ/NONE`. Project writes — `WRITE`, default
`HUMAN_EACH_EFFECT`, владелец может выбрать `HUMAN_SCOPED`. Все organization
writes требуют только `HUMAN_EACH_EFFECT`; alert delete — `DESTRUCTIVE` с
отдельным решением на каждый вызов. Full-organization grant разрешает все
текущие проекты организации в рамках точной capability; provider-derived
affected projects являются наблюдением, а не притворным atomic project-only
доказательством. EACH Gate показывает точные organization/version либо workflow
и bounded inputs. Доступ к organization mutations не появляется от одного
provider ID или project grant. Scoped consent связывает Connection,
org/project, exact `/issue_id` и `/status`, package/input/grant digest,
человека и срок; максимум24часа/100 effects, `ROOT_RUN` либо явно выбранный
`INITIATOR_RESOURCE`. Запись без решения не разрешается. Mutation не передаёт
неподдержанный upstream idempotency key и не имеет transport replay body.
Owner effect/input keys сохраняются только в штатной квитанции CP.

Исходный собственный provider subset:
`contracts/integrations/providers/sentry/v1/openapi.yaml`. Он описывает конечные
публичные операции и внутренние token metadata/project-scope GET; metadata не становится
отдельным инструментом сотрудника. Схема и реальные HTTP requests/responses
независимо проверяются consumer fixture, не только YAML parser.

## Полный жизненный цикл первой цепочки

| Переход | Authority, состояние и чтение |
|---|---|
| Create/configure | Owner назначает Connection; сохранение config не выдаёт grant и не добавляет native network pins |
| Add/test personal key | Self USER, exact connection/binding generation/key/config/package; metadata не создаёт global WRITE proof |
| Launch/claim | CP проверяет current root user, tenant, role grant/version и readiness; write требует scope observation, known deny блокирует до запуска |
| Each/scoped Gate | Решение owner для exact org/project/issue/status; чужое, истёкшее или несовпадающее решение не допускает effect |
| Begin/execute | Одноразовый Begin выдаёт только exact key revision и mandatory profile; provider consumer повторно проверяет current org/project/object/scopes |
| Complete success | Исходные lease/ref/fence/generation, bounded projection и effect/input/result digest; owner сохраняет receipt/audit и обычное continuation |
| Отказ до PUT | Foreign project, readonly scope, auth refusal или metadata failure возвращают отказ без fabricated effect receipt и без ложного UNKNOWN |
| Потеря mutation response/невалидный success/5xx | `UNKNOWN_OUTCOME`; один effect, чтение owner invocation сохраняет историю; автоматического повторного mutation и reconciliation по одному status нет |
| Cancel/delete/root terminal | Прежняя owner transaction закрывает authority/lease; отмена не доказывает отсутствие внешнего эффекта |
| Revoke/rotate/config/grant drift | Прежние owner locks/fences закрывают новый Begin и late completion; другого пользователя не затрагивают |
| Read lease expiry/retry | Только прежний read-only lifecycle с новым fence; старый claim не переиспользуется |
| Write lease expiry/retry | UNKNOWN не переходит в новое write исполнение; новый effect требует отдельного решения после сверки внешнего состояния |
| Restart/delegation/continuation | Устойчивый owner state, исходный root initiator, fresh RuntimeRevision и точные pins; process memory не источник полномочий |
| Dead-letter/WAITING_OWNER/CHANGES_REQUESTED | Новых фоновых task kinds нет; применяется существующий CP graph без частичного закрытия ресурсов |

Новых domain events нет: существующие invocation/credential audit, receipts,
continuation events и авторитетные owner read paths сохраняются для каждой
операции. Состояние provider status не доказывает происхождение потерянного mutation,
поэтому автоматический recovery этого effect закрыт.

Forward migration `20261114130000_sentry_personal_credentials.sql` следует за
GitHub scope migration `20261114120000`; она расширяет exact SQL checks и
существующий owner guard, сохраняет OID/ACL, запрещает empty-profile downgrade.
Старые миграции не редактируются. Полная normal/backup/immutable schema045
квалификация нового successor обязательна отдельно; отсутствие такой
квалификации не означает допуска релиза. Возврат image не откатывает БД;
оператор отключает новые grants/connections и оставляет forward-only schema.

## Проверка и ограничения доказательств

Consumer fixture охватывает все объявленные операции обоих origins, реальные
HTTP пути/методы/query/body/auth, provider schemas, bounded projection,
receipts, EACH/SCOPED, foreign-project отказ до PUT, readonly token, ambiguous
scope JSON и UNKNOWN без replay. Owner unit проверяет двух пользователей/
подключения, missing key/profile, revoke, expiry, rotation и configuration drift.
Эти локальные synthetic проверки не являются живой приёмкой.

Пока отсутствуют live targets и разрешённые credentials: проверка сотрудник →
provider → owner audit, browser после доставки, полная disposable PostgreSQL
квалификация migration и итоговый environment render нового candidate —
`NOT RUN`. Нет claims о CI, установке либо закрытии #2140.

Ручная проверка после qualified delivery: создать отдельные Cloud/self-hosted
connections, добавить личный токен, проверить project и READ observations,
выдать минимальный grant и попросить сотрудника прочитать project/issue/event.
Для записи выбрать exact synthetic issue/status Gate и сверить provider readback,
owner receipt и историю. Повторить с read-only/другим/revoked key, чужим проектом,
после restart и потери ответа. До этих действий live outcome остаётся `NOT RUN`.

Проверенные14.11.2026 источники: Context7 `/getsentry/sentry-docs`:
API permissions, pagination и region domains; `/getsentry/sentry`:
`src/sentry/api/endpoints/index.py` (собственные `auth.scopes`). Также проверены
[project](https://docs.sentry.io/api/projects/retrieve-a-project/),
[issues list](https://docs.sentry.io/api/events/list-a-projects-issues/),
[issue detail](https://docs.sentry.io/api/events/retrieve-an-issue/),
[status update](https://docs.sentry.io/api/events/update-an-issue/),
[event detail](https://docs.sentry.io/api/events/retrieve-an-event-for-a-project/).
Старые self-hosted версии без этого API contract не получают угадываемый fallback.


Проверенный pinned source26.10.1: [API routes](https://github.com/getsentry/sentry/blob/26.10.1/src/sentry/api/urls.py),
[issue comments](https://github.com/getsentry/sentry/blob/26.10.1/src/sentry/issues/endpoints/group_notes.py),
[project release semantics](https://github.com/getsentry/sentry/blob/26.10.1/src/sentry/releases/endpoints/project_release_details.py),
[organization releases](https://github.com/getsentry/sentry/blob/26.10.1/src/sentry/api/endpoints/organization_releases.py),
[workflow scope](https://github.com/getsentry/sentry/blob/26.10.1/src/sentry/workflow_engine/endpoints/organization_workflow_project_scope.py),
[workflow validator](https://github.com/getsentry/sentry/blob/26.10.1/src/sentry/workflow_engine/endpoints/validators/base/workflow.py),
[email action](https://github.com/getsentry/sentry/blob/26.10.1/src/sentry/notifications/notification_action/action_handler_registry/email_handler.py).
Comments и project-scope read имеют PRIVATE API status в этом source;
Cloud compatibility с ними не объявляется наблюдённой без живой проверки.
Legacy project `/rules/` в26.10.1 отсутствует; новые alerts используют
organization workflows. Эти различия не обходятся угадыванием API либо
redirect fallback.
