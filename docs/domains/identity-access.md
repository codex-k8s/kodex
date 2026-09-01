---
id: DOM-MC-002
title: Идентификация и доступ
type: domain
status: approved
owner: architect
version: 1.3.0
updated: 2026-08-29
---

# Идентификация и доступ

## Владение

Домен владеет `Organization`, `Subject`, registry разрешений, versioned
application role, access binding, OIDC group read model, политикой fresh OIDC
re-auth и audit attribution. MVP создаёт одну Organization, но каждый aggregate
и query сохраняет tenant boundary.

## Actor boundary

- OIDC issuer и subject разрешаются сервером в активного Subject; OIDC
  поставляет identity и bounded список групп, но не application permission.
- Browser payload не принимает actor, organization, owner, permission или root
  lineage.
- `auth_time`, `acr`, `amr`, issuer, subject и результат fresh re-auth берутся
  только из проверенной OIDC-сессии. Browser не может передать или повысить их
  request-полем.
- Opaque ref является locator, но не authority: ресурс сначала разрешается
  внутри tenant/project boundary, затем проверяются OCC и idempotency.
- Скрытый или чужой объект возвращает тот же безопасный результат, что
  отсутствующий.
- MCP-инструмент системного или обычного агента действует от имени сохранённого
  root actor только через signed context и не расширяет его permissions.

## Application RBAC

Policy является allow-only и вычисляется `control-plane` из четырёх частей:

- закрытый `permission_registry` с допустимыми resource kind и scope;
- `SYSTEM` и `CUSTOM` role с immutable версиями; изменение custom role создаёт
  новую версию, а существующие binding остаются pinned на прежней;
- binding на `USER`, `OIDC_GROUP` или `SERVICE` subject;
- scope `ORGANIZATION`, `PROJECT`, `RESOURCE_KIND` или `RESOURCE_INSTANCE` и
  bounded UTC-окно. `require_owner` допустим только для permission, который
  явно поддерживает owner condition.

System roles `OWNER`, `ADMINISTRATOR`, `OPERATOR`, `MEMBER`, `AUDITOR`
неизменяемы. Явного deny нет: отсутствие подходящего allow binding закрывает
операцию. OIDC group membership синхронизируется из каждого проверенного token
как read model; удалённая из token группа перестаёт участвовать в следующем
решении. Смена роли, binding или его состояния проходит OCC, semantic
idempotency, audit и transactional outbox.

Старый membership API не владеет полномочиями и не сохраняет отдельную write
model. Его команды транслируются в canonical custom role version и binding, а
read model является SQL view только над binding с server-owned presentation
marker. Узкие binding `RESOURCE_KIND` и `RESOURCE_INSTANCE` исключены из этой
проекции, поэтому presentation не расширяет exact scope до всего Project.

`RESOURCE_INSTANCE` связывает permission с точным server-resolved экземпляром,
его Organization и Project. Binding на один Agent, Session, Artifact, Secret,
RoleImage или RuntimeEnvironment не разрешает другой экземпляр того же kind.
Организационный либо проектный binding применяется к instance только после
разрешения target внутри той же tenant boundary. Переданный клиентом
`resource_kind`, parent ref или Project ref не участвует в доказательстве
scope.

Каждая защищённая команда и query используют один backend evaluator. Проверка
выполняется после разрешения target и включает:

1. активного Subject из проверенной OIDC или service identity;
2. точные Organization, Project, resource kind и resource instance из
   авторитетного состояния;
3. permission из закрытого registry, pinned role version, активные binding,
   owner condition и bounded UTC-окно;
4. требуемую fresh re-auth policy и одноразовый action proof, если операция
   чувствительная;
5. OCC, semantic idempotency и специализированный lifecycle transition.

Скрытие кнопки, route guard, capability из frontend state, наличие ссылки на
объект или ранее успешная проверка не заменяют повторную backend-проверку.
List, search, realtime subscription, detail, mutation и event catch-up
используют одинаковое eligibility rule.

`effective-access/query`, `explain` и `simulate` используют тот же evaluator,
что command authorization. Self explain сначала проверяет право видеть exact
target. Поэтому отсутствие и чужой ресурс возвращают одинаковый `NotFound`, а
denial не содержит refs неподошедших binding.

## Чувствительные разрешения и fresh OIDC re-auth

Permission registry содержит отдельные разрешения без объединения в общий
`manage` для следующих операций:

| Permission | Exact target | Fresh re-auth | Дополнительный инвариант |
|---|---|---|---|
| `secret.reveal` | Secret version | обязательна | одноразовый reveal grant, `no-store`, значение возвращает только `secret-broker` |
| `image.build` | RoleImage revision | обязательна | сборка только после policy/admission preview; permission не разрешает promotion |
| `image.promote` | успешно собранная RoleImage revision | обязательна | точный digest, provenance и допустимый target registry |
| `environment.privileged.manage` | RuntimeEnvironment revision | обязательна | effective resources, network и Kubernetes RBAC не превышают actor grants и installation admission policy |
| `prompt.full.view` | Session/Turn/Prompt revision | обязательна | отдельный защищённый read без secret values и provider credentials |
| `artifact.delete` | Artifact | не обязательна | только soft delete с отзывом новых binding и 30-дневным retention |
| `artifact.restore` | удалённый Artifact | не обязательна | восстановление только до `purge_after` в прежней tenant/project boundary |
| `artifact.purge` | удалённый Artifact | обязательна | необратимое удаление object versions после impact preview |
| `session.cancel` | Session или её active Turn | не обязательна | аварийная остановка остаётся доступной без step-up, но требует exact permission и server-owned lifecycle check |

Fresh re-auth подтверждается OIDC-провайдером не ранее чем за пять минут до
операции и должен удовлетворять минимальным `acr`/`amr` permission policy.
После callback `control-api-gateway` получает короткоживущий одноразовый action
proof, связанный с browser session, Subject, permission, exact target, nonce и
expiry. Proof нельзя использовать для другого target или permission; успешная
команда атомарно помечает его использованным. Обычный access token, обновление
страницы или локальный timestamp не являются step-up proof.

`artifact.delete`, `artifact.restore` и `session.cancel` намеренно не требуют
обязательной re-auth: первые две операции обратимы, а отмена Session является
операцией безопасности. Installation policy может усилить эти требования, но
не может отменить exact permission и backend lifecycle check.

## Service subjects и секретная граница

`secret-broker` получает отдельный `SERVICE` subject и минимальный
namespace-scoped Kubernetes доступ только к versioned Secret текущего Project.
Его binding не разрешает чтение других Project, управление RuntimeEnvironment
или выдачу permissions. `control-plane` хранит только descriptor, version,
rotation state и типизированный `display_hint`: не более 15% строкового значения
и не более 12 символов суммарно; короткие и binary значения не раскрываются.

Create/rotate/reveal требуют пользовательского application permission до
вызова `secret-broker`. Reveal дополнительно требует fresh action proof и
возвращается напрямую через одноразовый поток без PostgreSQL, cache, event,
audit payload, frontend store или observability. Audit фиксирует Subject,
permission, target, время и outcome, но не значение и не его фрагмент.

## Межсервисная авторизация

mTLS подтверждает workload transport identity, но каждый privileged RPC также
требует application credential, exact operation/permission, target binding,
короткий срок и durable replay protection. Caller-provided project или resource
ID не заменяет server-owned eligibility.

Service credential несёт только server-issued actor lineage и permission
request. Владелец target повторно разрешает resource instance и проверяет
permission; доверие к gateway, runner, assistant или `secret-broker` не
разрешает пропустить пользовательскую policy. Для команды помощника сохраняются
root Subject и двойная attribution, но решение вычисляется так же, как для
прямой команды пользователя.

JWKS и control-plane authorization snapshot допускают bounded last-known-good до
двух минут от последнего успешного получения. Ошибка повторного получения не
продлевает окно, а новый token не выдаётся дольше его остатка. Нарушение подписи,
rollback, conflict revision, истечение ключа или grace закрывают доступ сразу.
Краткая недоступность JWKS на старте не останавливает process и не меняет его
Pod readiness: verifier остаётся закрытым до первого успешного refresh, а
рабочий запрос получает типизированный `Unavailable`/HTTP `503`. Повреждённый
снимок не восстанавливается автоматически и требует контролируемого restart
после устранения причины.

## Инварианты

- disabled Subject не начинает новый effect;
- изменение role version и binding фиксирует audit без token/secret;
- отзыв binding или OIDC group закрывает новые HTTP, realtime и tool effects;
- permission на Project или resource kind не превращается в instance permission
  без server-side target resolution;
- fresh action proof нельзя повторить, перенести между targets или заменить
  browser-признаком успешного входа;
- membership presentation не является источником решения и не принимает
  самостоятельные записи;
- один eligibility rule используется для single, list, search и event path;
- Mattermost, GitHub и Kubernetes identity являются только external binding и не
  участвуют в core authority;
- пользовательский locale хранится как проверенное предпочтение и выбирает i18n
  сообщения, но не влияет на policy.

## События

Изменение role/binding создаёт `MEMBERSHIP_CHANGED` platform invalidation с
безопасным aggregate ref и отдельное audit event. Payload содержит version и
actor attribution и публикуется через transactional outbox.

## Критерии приёмки

- foreign project/run/session/gate/artifact не читается и не изменяется;
- binding `agent.launch` на один Agent в одном Project не разрешает другой
  Agent того же Project и Agent другого Project;
- explain чужого Agent возвращает `NotFound` без role/binding refs;
- system assistant не обходит права пользователя;
- audit позволяет отличить прямое действие от действия через помощника;
- `secret.reveal`, image build/promotion, privileged environment mutation,
  full prompt view, artifact delete/restore/purge и Session cancel независимо
  проверяются на exact target и не наследуются из UI visibility;
- soft-deleted Artifact восстанавливается только с `artifact.restore`, а
  физически удаляется только с `artifact.purge` и fresh re-auth;
- один browser WebSocket не сохраняет подписку после потери eligibility и не
  расширяет access при смене маршрута;
- token, cookie, JWS/JWK private material и secret value не попадают в ответы,
  логи, события и frontend.
