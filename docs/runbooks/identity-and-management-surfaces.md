---
id: RUN-MC-023
title: Identity и административные интерфейсы
type: runbook
status: approved
owner: sre
version: 2.0.1
updated: 2026-08-26
---

# Identity и административные интерфейсы

Kodex устанавливает Keycloak и три административные поверхности. Все внешние
маршруты кроме login/OIDC endpoints Keycloak закрыты OAuth2 Proxy.

| Интерфейс | Realm и role | Kubernetes authority |
| --- | --- | --- |
| Control Center | `kodex`, `kodex-owner` | собственная API authorization |
| Grafana | `kodex`, `kodex-owner` | auth-proxy header только от OAuth2 Proxy |
| Headlamp | `master`, `admin` | отдельный ServiceAccount `cluster-admin` |

Keycloak administrators намеренно получают полный доступ к кластеру через
Headlamp. Обычный owner realm `kodex` такого доступа не получает. Keycloak
Admin Console использует собственную аутентификацию, иначе возникла бы
циклическая зависимость от OAuth2 Proxy.

## Входные параметры

Имена обязательных полей перечислены в `.kodex-env.example`. Пароли
постоянного администратора и первого owner задаются владельцем. Bootstrap admin,
OIDC client secrets, cookie secrets и Grafana admin password генерируются
установщиком и сохраняются только в `.kodex-material`/Kubernetes Secrets.

## Установка

```bash
./install.sh --components cert-manager,identity,trust,management
```

Последовательность:

1. materialize identity/OAuth2 Secrets;
2. TLS PostgreSQL и публичного SSO через cert-manager;
3. Keycloak PostgreSQL и Keycloak;
4. realm, roles, clients, PKCE и постоянные пользователи;
5. monitoring stack и Headlamp;
6. три независимых OAuth2 Proxy;
7. exact Ingress/NetworkPolicy и readback.

## Readback

- публичные hosts уникальны и используют HTTPS;
- redirect URI каждого client равен только его `/oauth2/callback`;
- implicit/direct grants выключены, PKCE `S256` включён;
- access token не передаётся upstream административным UI;
- OAuth2 Proxy проверяет exact role;
- анонимный browser GET получает `302` в exact Keycloak issuer, тогда как
  прямой `/oauth2/auth` без сессии остаётся `401`, а отказ по role - `403`;
- Control Center не доступен в обход middleware;
- Headlamp ServiceAccount связан ровно с `cluster-admin`;
- Prometheus и Alertmanager не имеют публичного Ingress.

Initial passwords после первого входа необходимо сменить. Потерянные значения
восстанавливаются через owner-controlled процедуру Keycloak, а не чтением из
логов или GitHub artifacts.
