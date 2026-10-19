---
id: OPS-PROJECT-PURGE-1816
title: Завершение безвозвратного удаления проектов
type: operation-plan
status: approved
owner: manager
version: 1.1.0
updated: 2026-10-19
---

# Issue #1816

Источник: замечание владельца16.10.2026 и общий MVP checklist.
Единица — полный Control Plane backend path; основание PR#1815.

## Подтверждённое состояние

Два проекта имеют owner purge receipt PENDING от15.10, lifecycle
PURGE_PENDING, version3; активных Run/архивных tasks нет. У малого проекта
пять object receipts и одна сессия, у большого1704 receipts и442 сессии.
Ресурсы соседних установок и filesystem checkout не входят в удаление.

У малого проекта два inventory entries одного исторического archive key
созданы до штатного переноса общей сессии в проект. Server-owned lineage
уже привязан к проекту, но неизменяемый key остался под исходным `_system`.
Worker требует исключительно текущий project prefix и закрыто останавливается.
Кроме того, worker использует artifact store для archive receipts, хотя
архиватор пишет в отдельный bucket. Отсутствие объекта в неверном store не
является доказательством удаления архива.

## Контракт и жизненный цикл

| Переход | Полномочия и результат |
| --- | --- |
| Owner purge | Существующие UI/API/gateway/CP command, authority, OCC, idempotency; terminal всего project graph и durable receipt |
| Inventory | Тот же exact organization/project/receipt; незавершённый archive writer блокирует внешние эффекты |
| Artifact cleanup | Точный project artifact prefix либо persisted subject key/revision/version с artifact/run/session/conversation lineage перенесённого SYSTEM-диалога; path identity producer не подменяет root initiator или owner. Отсутствие foreign/shared owner, Delete и readback отсутствия |
| Archive cleanup | Отдельный archive store; exact persisted archive/task + session lineage и version. `_system` допустим только для подтверждённой перенесённой сессии, не по caller prefix |
| PVC cleanup | До effects paginated проверка всех точных inventory PVC consumers; current identity/hash и UID precondition. Прежний global hash допустим только для точной SYSTEM-session/conversation frozen проекта по тому же правилу promotion runtime-controller; чужие и активные ресурсы закрыты |
| Partial failure/retry | Никакого DONE; те же receipts/версии и идемпотентное отсутствие собственных ресурсов, без расширения scope |
| Finalize | Повтор inventory/receipt/project/active tasks/digest в owner transaction; защищённое удаление БД и существующее PROJECT_PURGED event |
| Two replicas | Exact-version object deletion и UID PVC precondition; единый owner receipt/transaction определяет terminal. Replacement прекращает текущую попытку; следующая заново проверяет принадлежность и consumers, а не повторяет DELETE прежнего UID |

Новые endpoint/grants/worker и ручной SQL UPDATE не вводятся. Используется
обычный периодический worker. Рабочие инструкции удаления, события и authority
не подменяются диагностическими сообщениями пользователя.

## Проверки

- [x] Unit: разные artifact/archive stores, moved-system archive, чужой/shared
  owner и version, active consumer, отсутствующий PVC, повтор и replacement UID.
- [ ] Disposable PostgreSQL: archive/task/session lineage, active task guard,
  terminal cleanup и сохранность чужого проекта.
- [ ] Применимые Go/config/render проверки и независимое review точного SHA.
- [ ] Code-first source rollout и штатное завершение ранее запрошенных purge.
- [ ] Chrome: screenshot корзины, Console/Network; owner readback и backend logs.

Текущие состояния — read-only наблюдение, не успешное удаление. Секретные
значения, object keys, содержимое файлов и личные данные здесь не сохраняются.

## Журнал

На6ab83711: полная CP Go suite PASS; полная disposable PostgreSQL suite
PASS159.160s и Avatar suite PASS; read-only worker/runner checks PASS.
`scripts/tests/web-only-release-test.sh` PASS. Общий
`make test-web-only-release` FAIL на устаревшем `rootCAsSecrets` ожидании
PWA test при актуальном `rootCAs`: тот же FAIL воспроизведён на основании
4147a9d7, TLS в этом unit не меняется.

Независимые review6ab83711 обнаружили P2: исходные global PVC и subject
result artifacts после Move→Purge без нового turn. Добавлены точные
server-owned lineage проверки и положительные/отрицательные regression
tests; финальные результаты ниже фиксируются после запуска.

Review1ed5848 выявил дополнительное ошибочное предположение: location
runtime artifact содержит identity producer, а created_by — root initiator.
Условие совпадения этих разных actor удалено; authority по-прежнему требует
exact immutable receipt и полной текущей server-owned lineage. Regression
fixture создаёт результат через настоящий CompleteExecution и читает его
persisted receipt, не угадывая identity producer по initiator.

### Применение cd6a4aba

- PASS: полная CP Go suite на Go1.26.6; полная disposable PostgreSQL suite
  157.978s, Avatar1.889s, read-only worker/runner checks. Адресная регрессия
  moved archive/result и project trash/session archive — PASS3.699s.
- PASS: независимые product/security/architecture reviews точного cd6a4aba,
  без P0/P1/P2; review не заменяет живую приёмку.
- PASS: source-only code-first rollout CP/API gateway с сохранением двух
  реплик, публичного SSO, sidecars и сети. Spec readback и host/Pod hashes
  worker/owner queries совпадают; CP обе реплики2/2, рестартов0.
- PASS: малый из двух ранее подтверждённых проектов удалён обычным worker;
  durable receipt DONE, удалено126 строк, project отсутствует. Корзина
  обновилась по WebSocket без reload страницы.
- PASS: другой контрольный проект и его файл доступны200;42 байта и digest
  не изменились. Runtime warm Pod и шесть остальных PVC сохранены.
- FAIL: большой проект после external cleanup остановлен на owner finalize:
  `prepared content cleanup must finish before project purge`. Никаких
  ручных SQL UPDATE, вызовов финализации вне worker или подмены DONE нет.
- У большого проекта два подготовленных объекта ADOPTED и один
  WAITING_OWNER с10.10, cleanup_attempts3, без object version/ETag.
  Worker artifact-retention установлен2/2, но этот terminal waiting state
  автоматически повторно не забирается. Пустой HEAD неизвестного Put не
  разрешает объявить подтверждённое удаление либо подделать receipt.
- NOT RUN: dedicated active-writer PostgreSQL regression и полная приёмка
  удаления обоих живых проектов; общий PWA assertion FAIL описан выше.

Продолжение требует отдельного штатного аудируемого repair/resolve для
неопределённого prepared intent с явным решением владельца. Нельзя лечить
это отключением guard либо ручным переводом WAITING_OWNER/CLEANED.
После разрешения этого состояния отдельно проверить finalization ADOPTED
ledger и сохранность чужого проекта. До этого Issue и checklist не закрыты.

## Решение владельца19.10: подтверждаемое восстановление

Владелец выбрал вариант1. Полные units: CP — Issue#1816/PR#1817;
gateway — #1818; PWA — #1819. Разработка идёт в отдельных worktree/PR,
контракты и порядок зависимостей CP→gateway→PWA заданы здесь. Merge/ready
не разрешены. Действующее WAITING_OWNER не снимается самим агентом.

Решение — отказ владельца от неопределённого результата с сохранением
обязательства очистки. Это не CLEANED и не доказательство отсутствия bytes.
Используется surviving prepared_content ledger, без второго Put либо поиска
по другим buckets. Новый OWNER_ABANDONED запрещает adopt/link/replay результата.
Минимальные storage/key/digest/size pins остаются worker после удаления проекта;
filename, media type и иные пользовательские metadata удаляются.

### Сквозной контракт

1. Owner/admin → GET `/api/v1/projects/{projectRef}/purge-recovery` →
   `PlatformQueryService.GetProjectPurgeRecovery` → domain service → CP
   единый read-only SQL snapshot. Actor/org из проверенного transport, не body.
2. Preview `ProjectPurgeRecovery`: `projectRef`, `version` (durable recovery
   version receipt), `snapshotDigest`, `state`, `unknownContentCount`,
   `cleanupPendingCount`, `projectPresent`, `canResolve`, `resolutionRef`,
   `projectVersion`. Ключи объектов/имена файлов и содержимое не выдаются.
3. Человек читает предупреждение и отдельно подтверждает потерю результата.
   POST того же route с `snapshotDigest`, `acknowledgeUnknownOutcome=true`,
   If-Match recovery.version, Idempotency-Key и CSRF →
   `PlatformCommandService.ResolveProjectPurgeRecovery` → закрытая command
   `RESOLVE_PROJECT_PURGE_RECOVERY` → owner transaction.
4. Owner lock order: receipt→project при наличии→exact ledgers; scope/роль
   проверяются до OCC/idempotency effect. Нужны frozen project, terminal
   источник, expired writer, отсутствие cleanup lease/adoption, exact digest
   и generation. Решение, новая generation, immutable resolution receipt,
   audit/idempotency и PROJECT_CHANGED существующего проекта атомарны.
5. Тот же CP maintenance lifecycle исполняет отдельный bounded claim этих
   обязательств. Storage identity неизменяема; exact positive receipt
   сохраняется до Delete, после Delete проверяется отсутствие exact version.
   Состояние `OWNER_ABANDONED` само по себе не выдаёт completion физической
   очистки. После удаления проекта read/повтор доступен owner через surviving
   purge receipt и ledger origin, не через воскрешение project.

| Переход | Результат и evidence |
| --- | --- |
| Preview / stale / чужой actor | Нет mutation, object I/O, audit/event; чужой ресурс скрыт, stale resolve отклонён |
| WAITING_OWNER → owner decision | OWNER_ABANDONED + durable exact resolution; не deletion receipt |
| Claim / две реплики | SKIP LOCKED, bounded lease, monotonic generation, immutable target; один текущий claimant |
| HEAD404 | OBSERVING и следующая ограниченная проверка; не CLEANED, без TTL удаления descriptor |
| Positive matching HEAD | Exact version/ETag pinned до Delete; mismatch ничего не удаляет |
| Delete + exact readback404 | DELETED_CONFIRMED для physical cleanup; generation/fence обязательны |
| Timeout/crash/lease expiry | Descriptor/approval сохраняются; новая попытка fenced, без второго Put |
| Retry после исчерпания | Только новое явное owner действие с fresh preview/OCC; original approval immutable |
| Project purge | Только resolution-backed detach; cleanupPending честно остаётся в surviving receipt/read path |
| ADOPTED purge | Ранний scrub по exact OBJECTS_CLEARED/inventory proof, до detach/revision DELETE; не fake artifact retention state |

Фоновые технические переходы не создают новых внешних domain events:
авторитетный read path — recovery snapshot surviving purge receipt; UI
подтверждение и обычный PROJECT_PURGED используют прежний WebSocket path.
Смена версии snapshot не принимается как новое согласие автоматически.

### Checklist продолжения

- [ ] CP: forward-only schema, owner query/command, полный worker lifecycle,
  known/unknown receipt, late Put, isolation и ADOPTED finalization.
- [ ] Gateway #1818: OpenAPI, exact RPC/client profile/authority, generated
  adapters и negative HTTP/CSRF/OCC/idempotency.
- [ ] PWA #1819: понятная причина в корзине, preview/modal/acknowledgement,
  outside close, stale refresh без повторного согласия, realtime и mobile.
- [ ] Применимые unit/disposable PostgreSQL/codegen/render/frontend checks,
  три направления review и source/image readback разрешённого rollout.
- [ ] Chrome screenshot/Console/Network и контроль чужого проекта. Человек
  сам подтверждает живое разрешение неизвестной загрузки; до этого NOT RUN.

### Проверки реализации19.10

Первый целевой disposable PostgreSQL прогон: PASS6.947s —
`TestProjectPurgeRecoveryComponent`, `TestPreparedContentBindingComponent`,
`TestProjectTrashBoundaryComponent`; все forward migrations и повторный up
до20261019000100 — PASS. Это синтетическая проверка, не выполнение решения
владельца над живым проектом. После неё уточнён общий бюджет claim, включая
истёкшие попытки; итоговый прогон фиксируется ниже на точном SHA.

Полная CP Go suite и vet — PASS на текущем дереве. Contract registry7 и
release migration CLI22 — PASS. Промежуточные ошибки запуска из неверного
cwd/PATH и старое negative expectation policy97 исправлены, повторные
соответствующие проверки — PASS. Эти результаты ещё не заменяют проверки
итоговой ревизии и живого интерфейса.

Для policy проверен штатный codegen97. Установленный прототип использует
`trusted-cluster`: новый exact RPC registry приходит вместе с кодом CP и
gateway. Подписанные protected policy/key history не переписываются. SQL
документация PostgreSQL18 через Context7: SELECT, row locking и SKIP LOCKED;
проверены фиксированный порядок receipt→ledger и ограниченный claim.

Rollback исходников после первого owner resolution не является совместимым:
старый enum/guard не понимает OWNER_ABANDONED. Миграция forward-only; при
ошибке остановить новый claim и исправить forward, не откатывать resolution
либо подменять deletion receipt. До применения сохраняется отдельный private
CP dump с проверкой архива; это не полный platform restore drill.

Первый независимый review9bbab174/1c0fcaf3/85c1a86: product без P0/P1/P2;
security и architecture нашли по одному P2. Исправлены INSERT guards
начального ledger/receipt и справедливая очередь по ближайшему due вместо
неизменяемого времени запроса проекта. Новые forged INSERT и fairness двух
проектов проверены отдельными disposable регрессиями; окончательное review
после исправлений фиксируется отдельно, старое подтверждение не переносится.

Широкий `^Test.*Component$` на9bbab174 — FAIL370.584s: четыре старых
сценария использовали общую изменённую БД (rollback core prompt/поколение
egress), ещё две fixture не настраивали обязательный artifact scanner.
Фикстуры переведены на существующий isolated template helper; в двух точках
задан предусмотренный synthetic scanner. Рабочие security checks и assertions
не отключались. Повтор выполняется на исправленной ревизии.
