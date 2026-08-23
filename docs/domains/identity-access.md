---
id: DOM-MC-002
title: Идентификация и доступ
type: domain
status: approved
owner: architect
version: 1.1.0
updated: 2026-08-23
---

# Идентификация и доступ

## Владение

Домен владеет `Organization`, `User`, `Membership`, platform role, project
membership, typed permission policy и audit attribution. MVP создаёт одну
Organization, но каждый aggregate и query сохраняет tenant boundary.

## Actor boundary

- OIDC issuer и subject разрешаются сервером в активного User и Membership.
- Browser payload не принимает actor, organization, owner, permission или root
  lineage.
- Opaque ref является locator, но не authority: ресурс сначала разрешается
  внутри tenant/project boundary, затем проверяются OCC и idempotency.
- Скрытый или чужой объект возвращает тот же безопасный результат, что
  отсутствующий.
- MCP-инструмент системного или обычного агента действует от имени сохранённого
  root actor только через signed context и не расширяет его permissions.

## Роли

Platform roles: `OWNER`, `ADMINISTRATOR`, `OPERATOR`, `MEMBER`, `AUDITOR`.
Project membership добавляет закрытый набор permissions для configuration,
launch, gate resolution, artifact и audit operations. Последний active Owner
защищён от удаления и понижения.

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

- disabled User не начинает новый effect;
- изменение membership и permission фиксирует audit без token/secret;
- один eligibility rule используется для single, list, search и event path;
- Mattermost, GitHub и Kubernetes identity являются только external binding и не
  участвуют в core authority;
- пользовательский locale хранится как проверенное предпочтение и выбирает i18n
  сообщения, но не влияет на policy.

## События

`organization.created`, `membership.changed`, `membership.suspended`,
`permission.policy_changed`. Payload содержит безопасные refs, version и actor
attribution и публикуется через transactional outbox.

## Критерии приёмки

- foreign project/run/session/gate/artifact не читается и не изменяется;
- system assistant не обходит права пользователя;
- audit позволяет отличить прямое действие от действия через помощника;
- token, cookie, JWS/JWK private material и secret value не попадают в ответы,
  логи, события и frontend.
