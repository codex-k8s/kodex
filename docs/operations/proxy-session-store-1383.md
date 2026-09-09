---
id: OPS-DOC-1383
title: Защищённое хранилище proxy-сессий Control Center
status: approved
type: operations
owner: sre
version: 1.0.0
updated: 2026-09-09
---

# Защищённое хранилище proxy-сессий Control Center

Связь: #1383, #1031, GUIDE-DOC-003, OPS-DOC-1241. Владелец capability —
`management-surfaces`; имя Kubernetes-компонента — `proxy-session-store`.
Его Secret, CA, Service, NetworkPolicy и постоянный том принадлежат этому
компоненту. Это отдельная зависимость обычной установки и dev-профиля:
приложения по-прежнему могут доставляться source rollout, а образ Valkey
закреплён digest. Хранилище не использует БД Kodex или данные других сервисов.

## Причина и границы

На исходном cookie-store две proxy-реплики одновременно обновили один
одноразовый refresh token. Keycloak сохранил `refreshTokenMaxReuse=0` и
закрыто ответил `invalid_grant`; браузер получил foreign OIDC fetch ещё до
`renewAfter` BFF. Сроки proxy-cookie, access/idle/absolute BFF и Keycloak
не меняются. Согласование BFF между вкладками не может заменить блокировку
между proxy-репликами.

OAuth2-proxy v7.15.3 использует Redis-compatible хранилище с шифрованием
содержимого сессии и собственным ticket. Два proxy-экземпляра получают
общую блокировку, перечитывают актуальное состояние и только затем вызывают
provider refresh. Локальный contract test использует настоящий middleware
этой версии и настоящий TLS Valkey; provider в нём синтетический, поэтому
он не доказывает живую доступность Keycloak.

В этой версии vendor lock имеет lease **2 секунды**, ожидание — **5 секунд**.
Проверенный локальный refresh ограничен 75 мс; при provider-задержке за
пределами lease нельзя заявлять доказанную единственность. Это явная граница
vendor-профиля, не основание увеличивать reuse или разрешать fallback.
Локальный callback 2300 мс воспроизводит два refresh и один отказ; тест
`KNOWN_LIMITATION_slow_refresh_exceeds_vendor_lease_NOT_CLOSED` означает
подтверждение дефекта, а не успешную приёмку. Долгий refresh/авария после
внешнего эффекта остаются отдельным сценарием
#1388 до доказательства совместимого протокола.

## Сквозная карта и lifecycle

| Инициатор / boundary | Авторитетный путь | Результат и наблюдение |
| --- | --- | --- |
| Browser cookie → Traefik exact auth | oauth2-proxy → TLS/ACL store → lock/reload → Keycloak | Один accepted refresh сохраняет зашифрованное состояние; соседи читают новую revision. Доменного события Kodex нет; authoritative read — proxy `/oauth2/auth` и BFF session GET. |
| Неистёкший accepted credential, временный provider failure | Vendor validation текущего access token | Состояние и срок не расширяются. Допустимость зависит от настоящей validation; глобального fail-open нет. |
| `invalid_grant`, отсутствующая/повреждённая сессия | Vendor invalidation / delete exact ticket key | Закрытый отказ; новый вход только штатным OIDC. |
| Valkey outage / неверный TLS или пароль | `/ready` проверяет реальное соединение | Proxy становится unready; `/ping` остаётся проверкой процесса. Cookie-store fallback отсутствует. |
| Рестарт Valkey | PVC + AOF `appendfsync always` | Принятое состояние и удаление сессии переживают рестарт. `noeviction` запрещает случайное вытеснение; заполнение закрывает запись. |
| Dev reload / runtime config встречает redirect | Browser `redirect:manual` | Нет запроса на foreign OIDC URL. Dev poll прекращается, один hint запускает обычный session GET; hint сам не означает unauthorized и не вызывает PUT refresh. |
| TLS/credential cutover | Новый явный operator protocol | Не выполняется application rollout. Trust сначала распространяется и проверяется; старый trust/credential выводится только после readback. |

## Ресурсы и ограничения эксплуатации

`deploy/k8s/base/proxy-session-store` — переносимый Kubernetes base:
cert-manager CA/серверный сертификат, ConfigMap, PVC 1 GiB, Service,
StatefulSet с одной репликой и две точные NetworkPolicy. StorageClass выбирает
установка; hostPath отсутствует. Valkey принимает только TLS 1.2/1.3,
plaintext port выключен. OAuth2-proxy проверяет CA и точный DNS/SNI
`proxy-session-store.kodex-system.svc.cluster.local`; mounted CA содержит
только `ca.crt`, а не private key сервера.

Immutable Secret `proxy-session-store-auth-v1` создаёт только owner script,
из криптографического random, без argv/логов. Default user отключён. Proxy
имеет команды session/lock только для `_kodex_control_center_oauth2-*`;
probe — PING/INFO; backup — PING/SAVE. Lua-команды lock сохраняют ту же ACL
границу ключей. Ingress store разрешён только control-center proxy в том же
namespace; egress store закрыт. Probe ходит по loopback, поэтому Service
readiness не создаёт циклической зависимости от самого себя.

Liveness проверяет процесс. Readiness store проверяет PING, окончание загрузки
и успешную AOF-запись; proxy readiness дополнительно проверяет свой TLS/auth
путь. Один store не обеспечивает HA: при его замене возможна контролируемая
пауза доступа. StatefulSet `OnDelete` исключает неявный rollout при изменении
конфигурации. Для ротации сертификата нужен отдельный согласованный restart
с сохранением PVC и readback; cert-manager Secret update сам не доказывает,
что Valkey загрузил новый сертификат. Проверять сроки и Ready сертификатов,
available proxy, свободное место PVC и AOF status до каждой приёмки.

Backup-команда сохраняет зашифрованный RDB только в private owner file 0600.
Он не является разрешением восстанавливать старые сессии: такой restore мог
бы воскресить отозванное состояние. При потере authoritative session storage
нужны новый credential epoch и controlled re-auth; accounts/данные Kodex
не сбрасываются. Нет автоматического replay backup и удаления томов.

## Ограниченный staging-переход

Все команды выполняет SRE/root из точного merged checkout. `CONTEXT`, private
каталог 0700 и manifest компонентов выбираются по свежему readback. Не
использовать global `up`. CLI имеет только staging mutation profile.

```bash
node tools/release/proxy-session-store.mjs plan-install \
  --context "$CONTEXT" --output "$PRIVATE/install-plan.json"
node tools/release/proxy-session-store.mjs install \
  --context "$CONTEXT" --plan "$PRIVATE/install-plan.json" \
  --evidence "$PRIVATE/install.jsonl" \
  --confirm INSTALL-STAGING-PROXY-SESSION-STORE
kubectl --context "$CONTEXT" -n kodex-system rollout status \
  statefulset/proxy-session-store --timeout=300s
node tools/release/proxy-session-store.mjs plan-cutover \
  --context "$CONTEXT" --output "$PRIVATE/cutover-plan.json"
```

Перед cutover закончить активные QA окна и включить утверждённое ограниченное
окно обслуживания публичного Control Center, включая HTML/auth. Сохранить
before Deployment/security fingerprints и serving manifest. Plan проверяет
исходную proxy image, две реплики, прежние TTL и готовую собственную
зависимость. Apply проверяет cluster/namespace/Deployment UID/resourceVersion
и полный spec; CAS drift не обходится повтором или force.

```bash
node tools/release/proxy-session-store.mjs cutover \
  --context "$CONTEXT" --plan "$PRIVATE/cutover-plan.json" \
  --evidence "$PRIVATE/cutover.jsonl" \
  --confirm CUTOVER-STAGING-PROXY-SESSIONS-REAUTH
kubectl --context "$CONTEXT" -n kodex-system rollout status \
  deployment/oauth2-control-center --timeout=300s
```

`INSTALLED`/`APPLIED` не означают PASS login/rollout. После обеих Ready proxy
реплик открыть доступ и выполнить один legitimate OIDC sign-in в новом
browser context без старых control proxy chunks (reader не ослабляется): старый
cookie-store cookie несовместим с Redis ticket. Данные приложения сохраняются.
При UNKNOWN сначала читать authoritative metadata/journal, не повторять
команду и не генерировать новые credentials. Повторный install при уже
существующих ресурсах закрыто отклоняется; частичное восстановление оформляется
отдельным exact plan/code. Bootstrap management-surfaces всегда подключает
эту dependency только для control-center, а другие surfaces не меняет. Для control-center отключён автоматический
Helm rollback по старой истории: она могла содержать cookie-store; pending
release требует явного совместимого recovery.

Откат приложения PWA не откатывает store/schema/credential history.
Возврат proxy к cookie-store после перехода запрещён как обычный rollback:
это вернёт доказанную гонку. Допустим ticket-compatible forward fix либо
отдельный controlled re-auth protocol без восстановления старого state.

```bash
node tools/release/proxy-session-store.mjs backup \
  --context "$CONTEXT" --output "$PRIVATE/session-store.rdb" \
  --evidence "$PRIVATE/backup.json" --confirm BACKUP-STAGING-PROXY-SESSIONS
```

## Проверки и критерии закрытия

Локальный публичный профиль:

```bash
node --test tools/release/proxy-session-store.test.mjs \
  tools/release/proxy-session-store.cli.test.mjs
KODEX_PROXY_SESSION_RENDER_TEST=1 KODEX_OAUTH2_CHART_ARCHIVE="$CHART" \
  node --test tools/release/proxy-session-store.render.test.mjs
GOWORK=off GOMAXPROCS=4 KODEX_PROXY_SESSION_CONTAINER_TEST=1 \
  go -C tools/release/proxy-session-contract test -p 2 -count=1 -timeout 120s ./...
```

Chart должен совпасть с lock SHA в `infra/management-surfaces/charts.lock.json`.
Container harness создаёт только собственный loopback container с disposable
fixture TLS/ACL/AOF, бюджетом 90 секунд и cleanup собственного имени. Его
container UID 0 нужен для переносимости rootless Docker bind mount и не
изменяет production `runAsNonRoot`; реальные Kubernetes UID/fsGroup доказываются
отдельным staging readback. Не запускать эту оснастку с live DSN/credentials.

PWA: unit, typecheck/build, scoped lint и `e2e/dev-reload.fixture.config.ts`
в Chromium/Firefox/WebKit. Live acceptance после merge/rollout отдельно:
два таба на свежем legitimate state, natural proxy refresh и BFF renewAfter,
один coordinated BFF refresh, обе SESSION_READY, v2 ticket/reconnect,
неизменённый absolute ceiling, ноль unexplained browser errors. Controlled
business event delivery требует настоящего producer; cursor continuity не
заменяет его. NOT RUN/FAIL старых окон сохраняются. Локальные fake-provider
fixtures не являются live Keycloak PASS.

Документация проверена через Context7: OAuth2-proxy session storage,
Valkey TLS/ACL/persistence, MDN fetch redirect. Поведение lock и доступные
флаги сверены с upstream `oauth2-proxy/v7.15.3`, а chart — с 10.7.0 и exact SHA.

Негативный fixture auth redirect отдельно учитывает особенность Chromium:
CDP может показать отменённый foreign redirect request (`net::ERR_ABORTED`),
хотя native fetch уже разрешился `opaqueredirect`, а второй HTTP server не
получил запрос. Fixture связывает completion и redirect chain точным
одноразовым диагностическим идентификатором и проверяет ноль внешних effects,
pageErrors и consoleErrors. Это не wildcard-исключение live network reporter:
его существующие FAIL правила не меняются, и этот negative fixture не является
доказательством нуля raw requestfailed во всех браузерах.
