---
id: RUN-MC-020
title: Публичный Control Center direct-production prototype
type: runbook
status: approved
owner: sre
version: 1.0.1
updated: 2026-08-15
---

# Публичный Control Center direct-production prototype

## Назначение

Этот runbook материализует публичный owner UI на
`https://__MATTERCODEX_PUBLIC_HOST__` после успешного exact dark deploy и SSO
bootstrap. Он не собирает application images и не изменяет данные продукта.

Внешний `kodex-public` ingress завершает публичный TLS. Отдельный Envoy bridge
принимает только HTTP-трафик этого ingress и подключается к
`staff-control-center` по TLS 1.3 с exact SNI, доверенной CA и клиентским
сертификатом. Существующая mTLS-граница приложения не ослабляется, а браузеру
не требуется клиентский сертификат.

## Предусловия

- exact dark release успешно применён и прошёл readback;
- `staff-control-center` и `control-api-gateway` имеют Ready replicas;
- SSO из `RUN-MC-016` (`docs/runbooks/direct-production-sso.md`) доступен на
  `https://__MATTERCODEX_OIDC_HOST__`;
- `staff-control-center-ingress-client-tls` материализован cert-manager;
- DNS `__MATTERCODEX_PUBLIC_HOST__` указывает на production node;
- `letsencrypt-prod` и `kodex-public` доступны.

## Применение

Операция выполняется cluster-admin только из exact merged revision:

```bash
infra/direct-production/control-center/bootstrap.sh \
  --context EXACT_CONTEXT \
  --mode apply
```

До изменения публичного bridge скрипт запускает временный validation pod с тем
же digest-pinned образом Envoy и проверяет итоговый `envoy.yaml` через
`envoy --mode validate`. После успешного preflight скрипт server-side применяет
закрытый набор ресурсов, ожидает публичный Certificate и две Ready replica
bridge, затем проверяет по публичному URL `/readyz` и `runtime-config.json`.
Временные validation-ресурсы удаляются; secret values, TLS key material и OIDC
token не выводятся.

Повторный readback не изменяет кластер:

```bash
infra/direct-production/control-center/bootstrap.sh \
  --context EXACT_CONTEXT \
  --mode readback
```

## Ручная проверка

1. Открыть `https://__MATTERCODEX_PUBLIC_HOST__/` в чистом browser profile.
2. Начать вход и убедиться, что redirect ведёт только на
   `https://__MATTERCODEX_OIDC_HOST__/realms/mattercodex`.
3. Войти owner-учётной записью и сменить временный пароль.
4. Проверить загрузку dashboard, REST-запрос и WebSocket без mixed content.
5. Выйти и убедиться, что защищённые данные не остаются доступны после logout.

## Диагностика и rollback

- Certificate и ACME: `Certificate/control-center-public-tls` и временные
  `acme-http-solver` ресурсы в `mattercodex-system`.
- Публичная маршрутизация: `Ingress/control-center-public`.
- mTLS hop: readiness pod `control-center-public-bridge` проверяет тот же
  `/readyz`, что и публичный маршрут.
- При отказе удалить только `Ingress/control-center-public`: внутренний dark
  contour и legacy Mattermost продолжат работать. Bridge и Certificate можно
  оставить для диагностики либо удалить повторяемым manifest из этого каталога.

До отдельного owner gate не отключать legacy Mattermost и bot-service. Data
migration и retirement legacy transport выполняются по `RUN-MC-015`.
