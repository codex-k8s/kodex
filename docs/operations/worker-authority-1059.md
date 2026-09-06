---
id: OPS-WORKER-AUTHORITY-1059
title: Служебная авторизация EMAIL и Mattermost
type: operations
status: approved
owner: backend
version: 1.2.0
updated: 2026-09-06
---

# Границы

Источник: #1018, #1059, зависимые #1030/#1037/#1046. Используется существующий
internal-rpc-authority; нового сервиса, универсального grant либо сетевого
разрешения этот unit не вводит. EMAIL не получает права другого workload;
Mattermost остаётся optional в `web-with-mattermost`.

| Переход | Владелец и связывание | Результат / отказ |
| --- | --- | --- |
| Fresh install | Offline генератор выпускает индивидуальный ES256 key для точного workload | Private key доступен только его grant-agent, public key доставляется CP. Не является ротацией существующей установки. |
| Startup grant | Grant-agent принимает только закрытый workload registry; key ID связывает workload и generation | Подпись issuer/audience/SPIFFE/workload, TTL 4 минуты; atomic write и exact readback до readiness. Неизвестный workload или чужой key ID отклоняется. |
| Refresh / restart | Тот же workload и credential generation, свежие revision/JTI | Ротация grant не сбрасывает принадлежащий CP durable high-watermark; rollback отклоняет CP. |
| Authority proof | EMAIL: три platform.email операции; Mattermost: восемь platform.interactions операций из producer profile #1046 | mTLS + application grant + local issuer → CP resolver → signed context → exact RPC. Grant не назначает project, actor либо invocation из произвольного payload. |
| Issuer state | Индивидуальный LOGIN, NOINHERIT/NOBYPASSRLS, CONNECT и SET только issuer capability | Readback/restore и session_user binding используют существующую owner-модель. Доступ к publisher/verifier capability не выдаётся. |
| Доставка issuer keys | Publisher меняет только точные имена Secrets, назначенные target registry | Readback/restore credentials и possession keys относятся к тому же workload/role; wildcard Secret access не добавляется. |
| Отказ / revoke | Недоступный ключ, issuer, trust, generation, restore fence либо CP | Закрытый отказ; отсутствие mounts не считается готовностью. Повтор business effect не добавляется. |

Новых business events нет. Авторитетный read path: существующий publisher
target registry, readback/restore state, CP grant watermark и typed owner RPC.
CP SQL handlers, operation policy, worker trust configuration и watermark
constraint принадлежат #1046. EMAIL mounts и consumer принадлежат #1037;
Mattermost consumer принадлежит #1030.

# Установка

Изменение SQL относится к поддерживаемому fresh-install baseline authority.
Это не inplace upgrade живой БД. Disposable deployment выполняется только
после полной интеграции. Запуск генерации либо materialize-secrets против
живой установки этим unit не разрешается.

Новые key/material refs не являются секретными значениями. Тесты используют
временные каталоги и синтетические ключи, не печатают JWK/grant/DSN.
Private key не передаётся через public trust. Почтовые порты и live mail
egress остаются вне этого unit.

# Проверка

До handoff необходимы local grant issuance/rotation/readback и negative
workload/key tests, fresh key separation, issuer profile, SQL principal
проверки и install/render assertions. Защищённый сквозной consumer → CP путь
проверяется на совместном дереве #1030/#1037/#1046/#1059; unit tests не
подменяют эту проверку. Общий review выполняется на итоговом SHA по #1018.

Публичные точки входа:

- В `services/internal/internal-rpc-authority`: `go test -race ./... -count=1`,
  `go vet ./...`, `go build ./...`. Включены запуск offline key generator,
  индивидуальность 11 ключей, отсутствие private material в public trust,
  точные права файлов, signer rotation и отрицательные identity/key случаи.
- `make test-internal-rpc-authority-postgres`: одноразовая PostgreSQL,
  два фактических `goose.UpContext` с неизменной migration history,
  реальные LOGIN/CONNECT/SET issuer, запрет других authority capabilities.
- `make test-install-contract`: полный набор статических и динамических
  проекций обоих consumer и замкнутый реестр из 22 runtime principals.
- `make test-worker-authority-projections`: два итоговых профиля,
  target revision 7, точные семь publisher Secret permissions и входящий
  CP/readback/restore/PostgreSQL путь. Проверка не заявляет готовность самого
  EMAIL consumer до интеграции #1037/#1046.
- `make test-web-only-release` и `make test-internal-rpc-authority-abi-render`:
  существующие проверки release render и совместимости authority sidecars.

Проверена актуальная документация Goose через Context7 `/pressly/goose`:
`UpContext`, транзакционное применение SQL и директивы `StatementBegin`.
SQL остаётся частью единственного fresh-install baseline, не миграцией
существующей живой установки. Результаты локальных запусков привязываются
к точному SHA в PR по #1059; защищённый consumer → CP и live provider до
общей интеграции имеют статус `NOT RUN`.

# Нормализация общего authority unit

Источник дополнения — интеграционный commit
`4e327e510bc6ffca5850d31d64663e5fee57e868`, producer #1046 и runtime
consumers #1025/#1026. Этот же PR получает принадлежащие authority Proto,
generated client/server signature и общий request-bound stream adapter.
Реализация CP/controller/runner остаётся в соответствующих unit.

`IssueContinuationAuthorizationContextResponse` выделен в собственный Proto
тип, сохраняя прежние wire field numbers1–8 и типы. Сервер и generated клиент
используют один новый signature. Это изменение source API требует согласованной
сборки; оно не меняет tenant/actor inheritance либо выданные полномочия.

| Переход stream | Проверка | Результат |
| --- | --- | --- |
| Открытие client handle | Exact зарегистрированный server-stream method; один initial request | Сетевой stream ещё не открывается. |
| Первый SendMsg | Детерминированный request digest, operation proof и issuer context | Только после успешного выпуска открывается stream и отправляется запрос. |
| Первый server RecvMsg | mTLS peer, обязательный bearer context и digest фактически декодированного Proto | Verified context публикуется до вызова owner, при несовпадении запрос отклонён. |
| Повторный initial request | Закрытая single-request форма | FailedPrecondition; второй owner effect не создаётся. |
| Отказ/EOF/cancel | Caller deadline и cancel/join | Child context закрывается; partial owner результат не считается подтверждённым. |

Новых business events нет; авторитетный read/receipt принадлежит конкретному
CP RPC. Shared adapter не выдаёт прикладные полномочия сам. Проверки issuance
до открытия stream, proof denial и проверки actual initial message находятся
в `libs/go/internalrpcauth/authorityclient/request_bound_stream_test.go`.
Proto lint/build и canonical generation/readback проверяются вместе с полными
race/vet/build обоих authority Go modules. Результаты привязываются к итоговому
SHA в PR; интеграционный baseline и live consumers остаются отдельными.

На production tree дополнения локально **PASS**: `make lint-proto build-proto`,
`make gen-proto check-proto-codegen`, полные race/vet/build shared authority и
сервиса, diff check. Восемь source/generated файлов побайтово совпадают с
исходным integrated4e327; generated получены канонической генерацией.
Приватные безопасные логи: `authority-normalize-proto.log`,
`authority-normalize-codegen.log`, `authority-normalize-go.log`.
Install/render для680c — **NOT RUN**: их предыдущий PASS на0765 относится
к прежнему checkpoint. Общий baseline/review/live также **NOT RUN**.

На `bf93ff2dea391aa80967f55da591327b8cca6196` публичный
`make test-internal-rpc-authority-postgres` — **PASS**. Оснастка выполняет
встроенную миграцию через тот же Goose API, что production CLI, дважды
проверяет текущую версию и неизменность истории. Компонентный тест завершился
за0.167s; последующие LOGIN/CONNECT/SET, отказ чужих capabilities, static
identity и exact promotion assertions также прошли. До этого добавления
публичная оснастка исполняла SQL через `psql`; её прежний PASS не доказывал
повторный Goose up. CLI unit, shell syntax и diff check — **PASS**.
Production Go/Proto и SQL migration в этом дополнении не изменялись.
Безопасный локальный лог: `authority-goose-postgres.log`.

Актуальная документация Goose `UpContext`, `SetBaseFS` и applied version
readback повторно проверена через Context7 `/pressly/goose`. Тест принимает
только порт созданного публичной оснасткой loopback контейнера; произвольный
DSN не принимает. Production CLI сохраняет обязательный exact verify-full TLS.
Эта проверка новой disposable установки не является проверкой обновления
существующей живой БД и не разрешает редактировать применённую миграцию.
