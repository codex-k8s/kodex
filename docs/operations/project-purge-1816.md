---
id: OPS-PROJECT-PURGE-1816
title: Завершение безвозвратного удаления проектов
type: operation-plan
status: approved
owner: manager
version: 1.0.0
updated: 2026-10-16
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
