---
id: OPS-DOC-NATS-STARTUP-1895
title: Закрытая диагностика подключения API к NATS
type: operations
status: approved
owner: manager
version: 1.1.0
updated: 2026-10-25
---

# Диагностическая граница

В [#1895](https://github.com/codex-k8s/kodex/issues/1895) обе новые реплики API
однократно завершились с прежней фиксированной ошибкой подключения NATS, затем
стали Ready. Причина этих initial failures остаётся **UNKNOWN**. Диагностика
сохраняет подтверждённый initial `no_servers`; узкое service-owned ожидание
теперь допускает повтор только этого класса. Сетевой root cause не утверждается.

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
wrapper; она не повторяется, проверка сертификатов не ослабляется.

## Матрица жизненного цикла

| Сценарий | Источник / authority | Исход и consumer |
| --- | --- | --- |
| Missing/invalid CA, client key/certificate либо credentials | Прежние config paths и SDK option parsing | Закрытый точный stage и filesystem/configuration class, nil connection, прежний exit1 |
| Auth/TLS connect rejection | Прежние mTLS/JWT/SNI/CA и NATS handshake | Приватная typed cause; наружу closed class, без Ready и business subscriptions |
| CONNECT/no_servers | Exact marker stage и closed classification всего error tree после permanent precedence | Синхронная пауза1s и новая попытка в прежнем startup context; last cause сохраняется |
| Dial failure другого класса / unknown | Прежний SDK connect | Одна попытка с closed class без угаданной причины; retry не разрешён |
| Connect успешно, flush отказал | Прежний `FlushTimeout(2s)` | Connection закрывается в прежнем месте, marker verify, exit1 |
| Connect/flush успешно | Прежний NATS client | Прежние revocations/browser-state/realtime checks; HTTP workers запускаются после существующего local barrier |
| Established disconnect/reconnect | Прежние callback handlers и SDK policy | Поведение и тексты прежних transition logs не меняются |
| Cancel / deadline до attempt или во время паузы | Прежний startup context Run | Новая попытка не допускается; last cause и closed wait_status=canceled/deadline сохраняются раздельно |
| Success после завершения context | Свежая проверка после synchronous constructor | Conn закрывается до передачи Run, HTTP/workers не запускаются по этому успеху |
| Shutdown / partial startup | Прежние defers / bounded HTTP cleanup | Новых workers, socket owners, grants или внешних writes нет |

`errorclass.TLS` — SDK-neutral извлечение четырёх уже существовавших typed
certificate checks. Control-plane использует thin delegate вместо дубликата;
его PostgreSQL priority, CLI bytes, startup policy и retry allowlist не меняются.
NATS sentinels и service policy остаются внутри API. Это единственное общее
извлечение в unit, без новой NATS-зависимости у observability.

## Ограничение startup budget

`connectNATSStartup` получает прежний startup context, созданный Run до
подключения dependencies. Новый budget не создаётся и не сбрасывается. Лишь
Прямой constructor marker `CONNECT + no_servers` без budget/вложенного marker
допускает повтор
после отменяемой паузы1s. Flush, auth, TLS, configuration, filesystem, unknown и
неразмеченный error не повторяются. После любого synchronous результата context
проверяется ещё раз; поздний успешный Conn и partial result закрываются до
возврата. Только допустимый success передаётся прежнему Run/defer Close.

`wait_status` — отдельный closed исход ожидания. `errors.Is/As` сохраняют
исходную причину и context cancellation/deadline; завершение budget не прячет
более позднюю permanent-причину. Внешние wrapper/join и вложенные diagnostic markers
не допускаются к retry: реальный constructor возвращает один прямой marker.
Классификация всего error сохраняет permanent precedence. Error/Format не выводят
raw cause даже для terminal Join после его безопасной нормализации.

**Hard mid-probe cancellation не исправлена.** One-shot `connectNATS` и его
SDK options остаются прежними: SDK разрешает имя без caller context, затем
выполняет dial и отдельно ограничивает protocol/TLS handshake. Outer loop
ограничивает допуск новых attempts, но не обещает прервать такой probe точно
в момент общего deadline. После его возвращения deadline success не принимается.
`RetryOnFailedConnect`, custom dialer, новые goroutines/framework и изменение
established reconnect отсутствуют. Последующий Ready не доказывает причину
первоначального отказа. Полная hard-deadline гарантия не заявляется.

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
wrong SNI, rejected JWT, missing/malformed credentials и malformed CA. Дополнительно
реальный rejected JWT даёт ровно одну startup attempt; реальный остановленный
broker даёт initial no_servers, после которого fixture запускает тот же container
на прежнем disposable loopback binding, и startup завершается в одном процессе.
Новая endpoint после ошибки не подставляется. Unit
дополнительно проверяет safe formatting, private cause, unknown/no_servers,
приоритет permanent failures, retry-success, deadline/cancel, close-before-handoff
и прежние control-plane diagnostic paths. Это не live acceptance и не
доказательство mid-probe DNS/SDK cancellation.

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
