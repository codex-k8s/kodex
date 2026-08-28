---
id: DOM-MC-002
title: Идентификация и доступ
type: domain
status: approved
owner: architect
version: 1.2.0
updated: 2026-08-28
---

# Идентификация и доступ

## Владение

Домен владеет `Organization`, `Subject`, registry разрешений, versioned
application role, access binding, OIDC group read model и audit attribution.
MVP создаёт одну Organization, но каждый aggregate и query сохраняет tenant
boundary.

## Actor boundary

- OIDC issuer и subject разрешаются сервером в активного Subject; OIDC
  поставляет identity и bounded список групп, но не application permission.
- Browser payload не принимает actor, organization, owner, permission или root
  lineage.
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

`effective-access/query`, `explain` и `simulate` используют тот же evaluator,
что command authorization. Self explain сначала проверяет право видеть exact
target. Поэтому отсутствие и чужой ресурс возвращают одинаковый `NotFound`, а
denial не содержит refs неподошедших binding.

## Межсервисная авторизация

mTLS подтверждает workload transport identity, но каждый privileged RPC также
требует application credential, exact operation/permission, target binding,
короткий срок и durable replay protection. Caller-provided project или resource
ID не заменяет server-owned eligibility.

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
- token, cookie, JWS/JWK private material и secret value не попадают в ответы,
  логи, события и frontend.
