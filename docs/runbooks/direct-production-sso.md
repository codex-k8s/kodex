---
id: RUN-MC-016
title: SSO direct-production прототипа
type: runbook
status: approved
owner: sre
version: 1.1.0
updated: 2026-08-15
---

# SSO direct-production прототипа

## Назначение

Runbook описывает первичную установку, повторное согласование и аварийное
восстановление Keycloak для owner Control Center. Keycloak работает в namespace
`identity` со своей PostgreSQL и не входит в release-managed dark manifest
`mattercodex-system`: SSO должен быть готов до запуска OIDC consumers.

Публичные endpoints:

- `https://sso.kodex.works` - Keycloak;
- `https://control.kodex.works` - owner Control Center;
- realm `mattercodex` - пользовательская identity;
- realm `master` - только операторская автоматизация Keycloak.

SSO не заменяет доменную авторизацию MatterCodex. Gateway проверяет issuer,
audience, scope `mattercodex.owner`, realm role `mattercodex-owner`, сроки токена
и серверный session lifecycle. `control-api-gateway` обращается к issuer по
обычному DNS, TLS 1.3, фиксированному CA и exact `/32` egress.

## Secret contracts

`identity/keycloak-bootstrap` создаётся один раз и имеет точный набор ключей:

- `admin-username`, `admin-password` - только первичная временная
  bootstrap-admin identity Keycloak;
- `database-password` - PostgreSQL Keycloak;
- `owner-username`, `owner-email`, `owner-initial-password` - первичный owner;
- `organization-id` - исходная организация MatterCodex.

`identity/keycloak-admin-client` является постоянной automation identity и
имеет только:

- `client-id` со значением `mattercodex-sso-bootstrap`;
- `client-secret` с высокоэнтропийным значением.

Значения не выводятся, не включаются в Git, rendered manifests, отчёты или
Mattermost. Наличие старых bootstrap credentials не означает, что временный
пользователь продолжает существовать.

## Первичная установка

Owner запускает из exact merge SHA:

```bash
umask 077
infra/direct-production/sso/bootstrap.sh \
  --context EXACT_CONTEXT \
  --mode apply \
  --oidc-ca-file /secure/path/oidc-ca.pem \
  --public-ipv4 EXACT_PUBLIC_IPV4 \
  --external-material-file /secure/path/external.yaml
```

На чистой установке скрипт:

1. создаёт `keycloak-bootstrap` без вывода значений;
2. запускает PostgreSQL и Keycloak;
3. использует временного bootstrap-admin только для одной операции;
4. создаёт confidential client `mattercodex-sso-bootstrap` с service account;
5. назначает service account composite realm role `admin` в realm `master`;
6. сохраняет credentials в `keycloak-admin-client`;
7. повторно входит через `client_credentials` и читает фактический role mapping;
8. удаляет пользователя, только если Keycloak явно пометил его атрибутом
   `is_temporary_admin=true`;
9. согласует OIDC mappers и CA binding.

Удаление существующего пользователя с тем же именем без temporary-атрибута
закрыто отклоняется.

Realm owner создаётся с переданными `owner-username` и `owner-email`, ролью
`mattercodex-owner` и обязательным действием `UPDATE_PASSWORD`. Временный пароль
передаётся владельцу только через отдельный закрытый канал. Добавление или отзыв
owner выполняется в realm `mattercodex`; оно не ослабляет дополнительную
доменную authorization boundary Control API.

Google Identity Provider включается отдельно после настройки exact redirect URI
`https://sso.kodex.works/realms/mattercodex/broker/google/endpoint` и
ограниченного first-login flow. Наличие Google OAuth не является prerequisite
для первого локального owner login.

## Повторный apply и readback

После первичной установки режимы `apply` и `readback` используют только
`keycloak-admin-client`. Password grant временного пользователя не является
fallback:

```bash
infra/direct-production/sso/bootstrap.sh \
  --context EXACT_CONTEXT \
  --mode readback \
  --oidc-ca-file /secure/path/oidc-ca.pem \
  --public-ipv4 EXACT_PUBLIC_IPV4
```

Readback подтверждает:

- точный Secret contract постоянного client;
- включённый confidential client без browser/password flows;
- service account и composite realm role `admin`;
- ровно один mapper `sub` и `realm roles` с утверждённой конфигурацией;
- точный OIDC egress CIDR;
- Ready certificate, PostgreSQL и Keycloak;
- issuer и непустой RSA JWKS.

После SSO readback обновлённый CA обязан быть сохранён и в
`ConfigMap/mattercodex-oidc-ca`, и в защищённом external-material source, иначе
следующий owner bootstrap вернёт старое значение.

## Аварийное восстановление automation identity

Recovery нужен только при утрате `keycloak-admin-client`, рассинхронизации его
secret или удалении role mapping. Смена пароля старого временного пользователя
не является восстановлением.

Операция требует отдельного owner gate и выполняется по официальному механизму
Keycloak `kc.sh bootstrap-admin service`:

1. Зафиксировать exact Git SHA, Kubernetes context и текущую реплику
   Deployment `identity/sso`.
2. Остановить **все** Keycloak nodes. Dedicated recovery command запрещено
   запускать одновременно с работающим Keycloak.
3. Сгенерировать одноразовый client secret без завершающего перевода строки и
   передать его recovery Job только через временный Kubernetes Secret.
4. Запустить pinned Keycloak image с `bootstrap-admin service`, отдельным
   уникальным client id и существующим PostgreSQL binding.
5. После успешного Job восстановить исходное число реплик и дождаться readiness.
6. Через одноразовый client создать или исправить
   `mattercodex-sso-bootstrap`, назначить его service account realm role
   `admin` в `master` и выполнить Admin API readback постоянным client.
7. Удалить одноразовый client из Keycloak и временные Job/Secret из Kubernetes.
8. Запустить обычный `bootstrap.sh --mode readback` и пользовательский OIDC E2E.

При любой ошибке Keycloak возвращается к исходному числу реплик. До
подтверждённого постоянного Admin API временный client не удаляется. Recovery
credentials и access tokens не печатаются. Официальный источник процедуры:
<https://www.keycloak.org/server/bootstrap-admin-recovery>.

## Пользовательский E2E

После изменения SSO или token contract проверяются реальные browser boundaries:

1. disposable owner-user получает только `mattercodex-owner`;
2. OIDC authorization code flow с PKCE завершается на
   `https://control.kodex.works/auth/callback`;
3. `POST /api/v1/session` возвращает `204` и устанавливает session/CSRF cookies;
4. `GET /api/v1/projects` с сессией возвращает `200`, без сессии - `401`;
5. disposable user удаляется даже при неуспешном сценарии;
6. временные recovery clients, Jobs и Secrets отсутствуют.

Результат фиксируется без token, cookie, password, user id и иных secret values.

## Rollback

До пользовательского cutover удаление SSO ingress не влияет на legacy
Mattermost. PostgreSQL StatefulSet/PVC, `keycloak-bootstrap` и
`keycloak-admin-client` не удаляются при rollback приложения. При отказе сначала
ограничивается публичный route, сохраняется PostgreSQL PVC и собирается только
redacted состояние Certificate, Deployment и PostgreSQL.
