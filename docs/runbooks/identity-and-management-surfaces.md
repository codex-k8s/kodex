---
id: RUN-MC-023
title: Identity и административные интерфейсы
type: runbook
status: approved
owner: sre
version: 2.0.4
updated: 2026-09-08
---

# Identity и административные интерфейсы

Kodex устанавливает Keycloak и три административные поверхности. Все внешние
маршруты кроме login/OIDC endpoints Keycloak закрыты OAuth2 Proxy.

| Интерфейс      | Realm и role           | Kubernetes authority                     |
| -------------- | ---------------------- | ---------------------------------------- |
| Control Center | `kodex`, `kodex-owner` | собственная API authorization            |
| Grafana        | `kodex`, `kodex-owner` | auth-proxy header только от OAuth2 Proxy |
| Headlamp       | `master`, `admin`      | отдельный ServiceAccount `cluster-admin` |

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
7. точный внутренний маршрут OAuth2 Proxy к bundled Keycloak с сохранением
   публичного issuer и TLS identity;
8. exact Ingress/NetworkPolicy и readback.

## Readback

- публичные hosts уникальны и используют HTTPS;
- redirect URI каждого client равен только его `/oauth2/callback`;
- implicit/direct grants выключены, PKCE `S256` включён;
- access token не передаётся upstream административным UI;
- OAuth2 Proxy проверяет exact role;
- OAuth2 Proxy разрешает публичное имя issuer во внутренний ClusterIP
  `identity/sso`, проверяет исходный TLS/SNI и имеет egress только к pod
  Keycloak на объявленный target port; корректность входа не зависит от
  hairpin NAT публичного адреса узла;
- анонимный browser GET получает `302` в exact Keycloak issuer, тогда как
  прямой `/oauth2/auth` без сессии остаётся `401`, а отказ по role - `403`;
- Control Center не доступен в обход middleware;
- Headlamp ServiceAccount связан с `cluster-admin` только через exact
  `ClusterRoleBinding/kodex-headlamp-admin`;
- Grafana готова как exact `StatefulSet/kodex-monitoring-grafana` в namespace
  `observability`; `Deployment` с таким именем не является допустимой заменой;
- Prometheus и Alertmanager не имеют публичного Ingress.

Initial passwords после первого входа необходимо сменить. Потерянные значения
восстанавливаются через owner-controlled процедуру Keycloak, а не чтением из
логов или GitHub artifacts.

## Передача обновлённых proxy cookies

При `cookie-refresh: 1h` OAuth2 Proxy возвращает обновлённую сессию через
`Set-Cookie` из `/oauth2/auth`. Traefik ForwardAuth переносит эти cookies в
ответ браузеру через `addAuthCookiesToResponse`. Для каждой поверхности
разрешены только её точный `_kodex_<surface>_oauth2` и chunks `_0`–`_3`.
Это альтернативные формы cookie, максимум четыре активных chunks; список
имён не разрешает OAuth2 CSRF, чужие сессии или перехват BFF cookies.
`authResponseHeaders` предназначен для заголовков upstream request и не
содержит `Set-Cookie`. Поддержанный maximum согласован с owner-session-storage.

| Сценарий | Источник authority и переход | Проверка результата |
| --- | --- | --- |
| Естественный refresh | OAuth2 Proxy проверяет существующую сессию и роль, обновляет credentials по прежней policy | Traefik переносит точные cookie и атрибуты; следующий запрос использует новое поколение |
| Разделённая cookie | Proxy владеет 2–4 chunks своей сессии | Переданы все разрешённые chunks; `_4`, login CSRF и чужие имена исключены |
| BFF renewal | BFF владеет своей session/CSRF парой | Ответ backend сохраняется, auth response не заменяет BFF cookies |
| Отказ proxy | Существующий отказ не обходит browser/API route policy | Нет автоматического обхода 401 или продления TTL |
| Неизвестный результат изменения middleware | Оператор фиксирует intent до CAS patch | Только authoritative readback, без слепого повторного apply |

Для уже работающего disposable staging предусмотрен отдельный ограниченный
переход только `kodex-system/oauth2-control-center-auth`; global `up` не нужен.
Namespace должен иметь `kodex.dev/environment=staging`. Plan закрепляет
cluster/namespace identity, Middleware UID/resourceVersion и весь исходный
spec; apply меняет только cookie list и проверяет итоговый spec digest.

```bash
node tools/release/proxy-session-cookies.mjs plan \
  --context "$CONTEXT" --output "$PRIVATE_PLAN"
node tools/release/proxy-session-cookies.mjs apply \
  --context "$CONTEXT" --plan "$PRIVATE_PLAN" --evidence "$PRIVATE_EVIDENCE" \
  --confirm PROPAGATE-STAGING-PROXY-SESSION-COOKIES
```

Команды выполняются SRE из точного merged checkout; private plan/evidence
создаются в новых файлах. Ни роль, ни TTL, ни существующие sessions/secrets не
изменяются. CAS drift требует нового read-only plan; UNKNOWN требует readback
без повторной mutation. Возврат к прежнему списку теряет refresh cookies и не
является обычным rollback: исправление дальнейшего дефекта выполняется через
новый PR/plan. Уже потерявшая обновление сессия может требовать законного входа;
свежий вход не доказывает исправление её прежнего отказа.

Локальные проверки: `make test-management-surfaces` и
`make test-pwa-public-assets-http`. Последняя использует настоящие закреплённые
Traefik/nginx в isolated network, синтетический auth и проверяет прежний
no-copy путь, basename/2–4 chunks, backend BFF и последующий запрос.
Она не доказывает конкретную причину исторического live401 и не заменяет
естественное часовое live-окно proxy refresh с UI/WS и BFF-сессией.

Документы проверены через Context7: `/traefik/traefik` ForwardAuth
`addAuthCookiesToResponse`/`authResponseHeaders` и
`/websites/oauth2-proxy_github_io_oauth2-proxy` integration cookie refresh.
