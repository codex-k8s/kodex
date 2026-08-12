---
id: RUN-MC-016
title: SSO direct-production прототипа
type: runbook
status: approved
owner: sre
version: 1.0.0
updated: 2026-08-12
---

# SSO direct-production прототипа

## Назначение

Профиль разворачивает Keycloak в namespace `identity`, отдельную PostgreSQL и
публичный issuer `https://sso.kodex.works/realms/mattercodex`. Control Center
использует Authorization Code + PKCE, а `control-api-gateway` проверяет тот же
issuer по внутреннему DNS-маршруту, TLS 1.3 и фиксированному CA.

Keycloak и его PostgreSQL не входят в release-managed dark manifest
`mattercodex-system`: SSO должен быть готов до запуска OIDC consumers. Все
изменения выполняются кодом из `infra/direct-production/sso`.

## Первый запуск

1. Убедиться, что `sso.kodex.works` указывает на публичный ingress кластера, а
   ClusterIssuer `letsencrypt-prod` готов.
2. Выбрать точный Kubernetes context и доверенный CA публичного сертификата.
3. Выполнить `infra/direct-production/sso/bootstrap.sh --mode apply` с exact
   context, `--oidc-ca-file` и защищенным external-material file из
   `RUN-MC-015`.
4. Дождаться readback discovery, JWKS, Certificate, PostgreSQL и Keycloak.

Скрипт генерирует `Secret/identity/keycloak-bootstrap` только при его отсутствии
и не заменяет существующие credentials. Secret содержит отдельные database,
bootstrap-admin и временные owner credentials, а также server-owned
`organization-id`. Значения не выводятся в лог и не хранятся в Git.

## Owner и администратор Keycloak

- realm owner создается с username `lepehovsv`, подтвержденным email и
  обязательным действием `UPDATE_PASSWORD`;
- временный пароль передается владельцу через отдельный краткоживущий закрытый
  канал и удаляется из него сразу после успешной смены;
- bootstrap admin используется только для `master` realm и настройки Keycloak;
- после создания постоянного именованного администратора bootstrap credential
  отзывается, а его временная копия удаляется;
- добавить или отозвать owner можно в realm `mattercodex` через Users и
  realm-role `mattercodex-owner`; Control API дополнительно сохраняет доменную
  authorization boundary.

Google Identity Provider включается отдельно после настройки exact redirect URI
`https://sso.kodex.works/realms/mattercodex/broker/google/endpoint` и
ограниченного first-login flow. Наличие Google OAuth не является prerequisite
для первого локального owner login.

## Readback

Повторный запуск с `--mode readback` не изменяет кластер и проверяет:

- закрытый key set bootstrap Secret без вывода значений;
- Ready публичного Certificate;
- Ready PostgreSQL и Keycloak;
- exact issuer и JWKS URI;
- непустой RSA JWKS.

После SSO readback обновленный CA обязан быть сохранен и в
`ConfigMap/mattercodex-oidc-ca`, и в защищенном external-material source, иначе
следующий owner bootstrap вернет старое значение.

## Rollback

До пользовательского cutover удаление SSO ingress не влияет на legacy
Mattermost. StatefulSet/PVC и bootstrap Secret не удалять: откат приложения не
является разрешением потерять realm, пользователей или credentials. При отказе
сначала убрать публичный route, сохранить PostgreSQL PVC и собрать redacted
состояние Certificate, Deployment и PostgreSQL.
