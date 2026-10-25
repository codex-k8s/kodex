---
id: OPS-DOC-NATS-STARTUP-1895
title: Закрытая диагностика подключения API к NATS
type: operations
status: approved
owner: manager
version: 1.0.0
updated: 2026-10-25
---

# Диагностическая граница

В [#1895](https://github.com/codex-k8s/kodex/issues/1895) обе новые реплики API
однократно завершились с прежней фиксированной ошибкой подключения NATS, затем
стали Ready. Причина этих initial failures остаётся **UNKNOWN**. Это изменение
исправляет подтверждённую потерю диагностики, а не гипотезу сетевого отказа.

Producer — прежний `control-api-gateway/internal/app.connectNATS`. Прежние
настроенные NATS URL, TLS1.3, exact SNI/CA, client certificate и user credentials
сохраняются. Три option wrappers переносят лишь ошибку прежнего применения
RootCAs/ClientCert/UserCredentials в marker точного этапа. Connect и flush
получают такой же marker. Приватная cause сохраняется для `errors.Is/As`;
`Error` и все format verbs выдают только закрытые stage/class. Прежний `%v` в
main теперь безопасно печатает эти NATS markers. Остальные Run errors и
диагностика соседних dependencies не переклассифицируются.

Этапы: `load realtime NATS CA`, `load realtime NATS client certificate`,
`load realtime NATS credentials`, `connect realtime NATS consumer`,
`verify realtime NATS consumer`; неизвестный stage — `realtime NATS startup`.
Классы выводятся из NATS и трёх typed NKeys credential sentinels,
typed TLS/filesystem/context failures и
общего `errorclass.Network`, без чтения текста ошибки. Неизвестное остаётся
`unknown`. Path/URL/host/JWT/seed/server message не выводятся. Runtime-строки
остаются английскими; новых metric labels, API/Proto/DB/events нет.

`nats.go v1.52.0` может свернуть первоначальный отказ dial в `ErrNoServers`.
Поэтому `no_servers` не доказывает `connection_refused` либо неисправность сети.
Unclassified alert или ошибка SDK также не становятся предположением об auth
или TLS. SDK `ErrTLS` даёт общий `tls_handshake`; typed verification остаётся точнее.
Постоянная auth/TLS/configuration-причина имеет приоритет над сетевым
wrapper; повторов либо ослабления проверки сертификатов не добавляется.

## Матрица жизненного цикла

| Сценарий | Источник / authority | Исход и consumer |
| --- | --- | --- |
| Missing/invalid CA, client key/certificate либо credentials | Прежние config paths и SDK option parsing | Закрытый точный stage и filesystem/configuration class, nil connection, прежний exit1 |
| Auth/TLS connect rejection | Прежние mTLS/JWT/SNI/CA и NATS handshake | Приватная typed cause; наружу closed class, без Ready и business subscriptions |
| Dial failure / no_servers / unknown | Прежний SDK connect | Closed class без угаданной причины; поведение первого connect сохраняется |
| Connect успешно, flush отказал | Прежний `FlushTimeout(2s)` | Connection закрывается в прежнем месте, marker verify, exit1 |
| Connect/flush успешно | Прежний NATS client | Прежние revocations/browser-state/realtime checks; HTTP workers запускаются после существующего local barrier |
| Established disconnect/reconnect | Прежние callback handlers и SDK policy | Поведение и тексты прежних transition logs не меняются |
| Cancel / deadline | Прежний process/startup lifecycle | Новой cancellation/retry схемы нет; диагностический marker не расширяет budget либо authority |
| Shutdown / partial startup | Прежние defers / bounded HTTP cleanup | Новых workers, socket owners, grants или внешних writes нет |

`errorclass.TLS` — SDK-neutral извлечение четырёх уже существовавших typed
certificate checks. Control-plane использует thin delegate вместо дубликата;
его PostgreSQL priority, CLI bytes, startup policy и retry allowlist не меняются.
NATS sentinels и service policy остаются внутри API. Это единственное общее
извлечение в unit, без новой NATS-зависимости у observability.

## Ограничение startup budget

Общий `StartupTimeout` сейчас не передаётся в `connectNATS`. `nats.Timeout(3s)`
не является единым budget: SDK сначала разрешает имя без caller context,
затем выполняет dial, отдельно ограничивает protocol/TLS handshake, после
чего API выполняет flush. Этот пробел **не исправлен** данным диагностическим
unit. `MaxReconnects(-1)` не добавляет retry первого connect; включать
`RetryOnFailedConnect`, менять dialer, TLS, contexts или reconnect lifecycle
в рамках этого изменения запрещено. Последующий Ready не доказывает устранения
первоначальной причины. Готовность полного startup/recovery решения не заявляется.

## Поддерживаемая локальная проверка

`make test-control-api-nats-startup` — отдельная disposable-проверка, не кластер.
Canonical image берётся из `deploy/k8s/base/platform-state/nats.yaml` и сверяется
с pinned fixture. Порт публикуется только на `127.0.0.1` со случайным host port;
1CPU/128MiB/64pids, read-only rootfs, cap-drop и no-new-privileges. Pull отсутствует:
при отсутствии image проверка закрыто отказывает. Скрипт ограничивает Go90s,
outer110s и cleanup; рекомендуемый внешний budget180s.

Одноразовые CA/client/server certificate и operator/account/user credentials
создаются в приватном каталоге штатными X.509, `nkeys` и `jwt/v2 v2.8.1` APIs.
JWT library импортируется только из `_test.go`; version и module sums закреплены.
Существующий runtime NKeys остаётся v0.4.15 без upgrades/replace. SDK принимает
недекорированный JWT при применении опции; отсутствие seed обнаруживается его
прежним signing callback. Диагностика не добавляет предварительную validation.
Production/runtime credentials и cluster не читаются. MEMORY resolver не создаёт
JetStream state. Проверяются настоящий JWT+mTLS success/flush/close, wrong CA,
wrong SNI, rejected JWT, missing/malformed credentials и malformed CA. Unit
дополнительно проверяет safe formatting, private cause, unknown/no_servers,
приоритет permanent failures и прежние control-plane diagnostic paths.

Ordinary Go unit не запускает Docker component автоматически; отсутствие его
явной точки входа — **NOT RUN**, а не доказательство TLS/NATS acceptance.
Fixture cleanup проверяет точные созданные container ID/name/label, затем
отсутствие ID и имени. При отказе сохраняется приватная оснастка; сырой NATS log
или JWT не печатается. Нельзя удалять контейнер только по похожему имени.

Context7 недоступен (`Invalid API key`); проверены официальные
[NATS Go SDK](https://pkg.go.dev/github.com/nats-io/nats.go), pinned SDK source
v1.52.0 и [NATS JWT API](https://pkg.go.dev/github.com/nats-io/jwt/v2).

## Ручная проверка и rollback

После отдельного разрешения владелец доставляет exact immutable API image,
сопоставляет Pod UID/source/imageID/restarts и новый закрытый diagnostic class.
Проверяет существующую историю, файлы и realtime reconnect без принудительного
DB/NATS fault и без повторения внешних действий. Отсутствие нового startup failure
не устанавливает причину прежнего. Rollback — прежний approved immutable API
image; он возвращает прежнюю меньшую диагностичность, не меняя данные или grants.
Ни rollout, ни ручная проверка не считаются выполненными по одним unit results.
