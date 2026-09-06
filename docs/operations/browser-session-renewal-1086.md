---
id: OPS-MC-1086
title: Backend OIDC renewal и browser session
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-06
---

# Backend OIDC renewal

Refs #1086, #1045, #1022, #1031. Документ задаёт реализацию исправления;
локальные проверки перечислены ниже, повторная приёмка и live — NOT RUN.

## Источник и границы

MVP-UI-11 требует server expiry, jitter, ограниченные повторы и новый
realtime ticket после renewal. Прежний PUT продлевал только idle cookie до
expiry сохранённого bearer. Backend refresh является отдельным протоколом:
он не расширяет абсолютную Keycloak SSO policy и не обновляет auth_time.

В действующем repo-owned Keycloak client kodex-control-center включены
Authorization Code и PKCE S256. Realm idle/max равны 8/12 часам, access
client override — 1 час. Это значения конфигурации репозитория, не evidence
живой установки. Refresh token и access token обрабатываются только backend;
PWA получает redirect/code flow и безопасную session metadata.

CP подтвердил сохранение issuer/sub/organization/sid/session_revision при
renewal той же сессии. Каждый свежий bearer проходит полную VerifyToken и
получает новый application proof. Несовпавший tuple требует нового входа;
старый proof не используется как доказательство свежей авторизации.

`sessionRevision` остаётся revision OIDC/CP. `generation` идентифицирует
browser session, а `version` — текущую durable family. Consumer отслеживает
пару generation/version для обновления WS ticket с прежним cursor. GET
metadata не продлевает idle. Legacy bearer login возвращает режим
`REAUTHENTICATION`, новый backend flow — `BACKEND_REFRESH`. После расхода
elevation в Reveal/Email прежний command payload не меняется: consumer читает
GET session после успеха или protected recovery, принимает новую пару
generation/version и получает новый WS ticket с прежним cursor.

Из advertised `OwnerSessionPurpose` удалены неисполняемые значения CREATE,
ROTATE и REVOKE: действующие secret commands не используют этот elevation
flow и не изменены. REVEAL и EMAIL остаются привязанными к точному ресурсу.
Отдельный `freshAuthentication` запускает настоящий IdP prompt=login/max_age=0
для существующей environment policy, но сам не выдаёт дополнительных прав.

## Карта и матрица lifecycle

| Инициатор | Владелец и проверка | Изменение и outcome | Consumer/readback |
| --- | --- | --- | --- |
| Browser login | BFF назначает state, nonce, PKCE и bounded login transaction; same-origin, cookie binding | Одноразовый code exchange с точным issuer/client/redirect; pending transaction до внешнего запроса | Browser получает только безопасный переход/результат, не tokens |
| Callback | BFF проверяет state/browser binding и ID-token nonce, access issuer/audience/actor tuple | Только один владелец attempt; неопределённый exchange не повторяется скрыто | Новая HttpOnly session и CSRF; versioned metadata |
| PUT renewal | Exact Origin и CSRF, текущая server session/family, idle/absolute expiry и current revision | Durable CAS выбирает одного владельца refresh; остальные читают committed generation | expiresAt/renewAfter/absoluteExpiresAt/version; PWA планирует jitter |
| Refresh success | Новый access token проверен полностью, identity tuple прежний; refresh token rotation сохраняется только encrypted | Generation и token snapshot фиксируются вместе до Set-Cookie | Новый одноразовый WS ticket; прежний event cursor сохраняется |
| Refresh transport uncertainty | Внешний token endpoint мог уже выполнить rotation | Не повторять тот же refresh token автоматически; закрытый outcome и явный re-auth | Ограниченный UI recovery, отсутствие бесконечного retry |
| Invalid grant/expired SSO | Никакого продления auth_time или абсолютной policy | Terminal family, credentials не возвращаются в browser | Один явный новый SSO flow |
| Logout/revoke | Durable family fence и existing browser revocation до очистки cookies | Поздний refresh/Set-Cookie не восстанавливает terminal family | Все replicas проверяют owner state; protected GET не поддерживает idle |
| Replica restart/store unavailable | Durable state/readiness с exact stream limits/ACL; нет in-memory authority fallback | Закрытый отказ, сохранённый terminal/high-watermark | Safe retryable error без token/PII |

Browser session хранит только прикладную authority; новое состояние token
family принадлежит BFF. Не вводится новый CP session RPC. Storage ciphertext
связывается с family/version/issuer и шифруется gateway key; runtime имеет
точные publish/read subjects, bootstrap остаётся отдельной identity.
Retention family/tombstone покрывает абсолютный срок, а не прежние 2 часа
idle-cookie revocation. State payload не выдаёт права произвольному actor.

Канонический stream `CONTROL_API_BROWSER_STATE` хранит только последнюю
зашифрованную запись каждого случайного UUID. FileStorage, exact replicas,
retention 13 часов, максимум 100 000 записей / 512 MiB / 64 KiB на запись;
`DiscardNew` запрещает вытеснение других family при заполнении.
`DiscardNewPerSubject=false` допускает замену своей записи с exact CAS;
delete/purge/rollup и caller TTL запрещены. Отсутствие family закрывает вход,
а не восстанавливает токены из cookie. Публикация выполняется без retry;
потерянный ACK разрешается только exact readback уникального ciphertext.

Context7 проверен также для `/nats-io/nats.go`; семантика per-subject
replacement сверена с `nats-server/v2@v2.14.0/server/filestore.go`:
лимит `DiscardNew` не вытесняет чужую запись, а CAS сохраняет одного
владельца конкретной версии. Runtime bootstrap/create права не получает.

## Проверки

Обязательны положительный refresh до/после короткого access expiry,
сохранение SSO absolute ceiling, конкурентные tabs/replicas, потеря ACK,
rotation/replay/OCC, неверные issuer/sid/nonce/CSRF, cancel/logout race,
подмена store snapshot, unavailable store и bounded retry. Full codegen,
HTTP/SDK/WS consumer и environment render проверяются на одном checkpoint.
Ни script, ни тест не обращается к live IdP без отдельного допуска.

`make test-browser-state-component` использует точный digest NATS из deploy
в disposable контейнере, публикует только на loopback и удаляет контейнер
после завершения. Бюджет pull 90 секунд, каждая из двух фаз — 60 секунд.
Первая фаза проверяет FileStorage/CAS/lost ACK/readiness, затем script
останавливает процесс через SIGKILL. Вторая фаза на прежнем private volume
проверяет terminal record и отказ старой sequence. При ошибке private evidence
сохраняется; окружение и реальные credentials не монтируются.

Context7 проверен для /keycloak/keycloak: Authorization Code + PKCE,
refresh-token rotation и SSO session timeout. Источники:
[Keycloak token endpoints](https://github.com/keycloak/keycloak/blob/main/docs/guides/securing-apps/partials/oidc/available-endpoints.adoc),
[RealmModel session policy](https://github.com/keycloak/keycloak/blob/main/server-spi/src/main/java/org/keycloak/models/RealmModel.java).

Локально на рабочем дереве исправления прошли полный gateway race/vet/build,
oidcverifier race/vet и проверка management surfaces. HTTP race — 6.162 с,
boundary race — 2.057 с, oidcverifier race — 1.291 с. После отдельного guard
завершения refresh за idle/absolute deadline session race повторён: PASS
1.023 с; expired attempt сохраняет terminal без credentials. Strict generated
SDK tsc завершён PASS. Canonical Go/TS/AsyncAPI generation выполнена;
AsyncAPI replay PASS. Web-only render содержит exact browser origin и новый
CP byte budget. Эти результаты ещё
не являются evidence неизменяемого итогового SHA всего unit.

Реальный component на NATS 2.14.4 завершён PASS: фаза записи — 1.024 с,
восстановление после SIGKILL — 1.018 с. Проверены единственный CAS-победитель,
восстановление потерянного ACK точным readback, сохранённый terminal record,
отказ устаревшей sequence и readiness при неверной retention. Два первых
запуска оснастки завершились FAIL: недоступный host bind volume и изменившийся
loopback port после restart. Исправлены managed disposable volume и повторное
чтение назначенного порта; production-протокол не ослаблялся.

Проверка NATS material выявила отдельный FAIL бюджета: прежняя reservation CP
и revocation уже занимали 32 GiB. Согласовано выделение 512 MiB для browser
state внутри прежнего account/server/PVC budget; CP MaxBytes становится
33806090240. Retention, message и dedup limits CP не меняются. Повтор проверки
материалов завершён PASS. Существующий `reconcileEmptyStream` отклоняет drift
любого непустого stream до UpdateStream: новая граница не удаляет сообщения
для освобождения места. Для непустого прежнего stream bootstrap завершится
закрытым отказом; автоматическая очистка не предусмотрена. Проверку этого
bootstrap companion выполняет CP unit; здесь она пока NOT RUN.

Локальный baseline предыдущего e1e8 не закрывает #1086. Полный новый baseline,
PWA consumer, общее review и live acceptance — NOT RUN.
