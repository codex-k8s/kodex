---
id: OPS-ARTIFACT-ANTIVIRUS-1814
title: Серверный антивирусный допуск точных байтов файла
type: operation-plan
status: approved
owner: manager
version: 1.0.0
updated: 2026-10-16
---

# Допуск файлов — Issue #1814

Источник: одобренный план `OPS-MVP-COMPLETION-20261016`, пункт допуска
файлов; <https://github.com/codex-k8s/kodex/issues/1814>.
Единица изменения — backend Control Plane с принадлежащей ему конфигурацией.

## Сквозной путь

Пользовательский upload/avatar проходит существующие endpoint, gateway, RPC,
principal и owner preflight. Runtime completion проходит проверенные workload,
lease/fence/generation и свежую owner-транзакцию. Подготовка файла помощником
сохраняет существующие profile/plan/version/lease pins и durable content ledger.
Ни один из этих путей не принимает результат сканирования от пользователя.

После preflight точный bounded reader передаётся локальному CP-owned clamd
через Unix INSTREAM. Сканер проверяет свежесть и неизменность версии базы,
вычисляет SHA-256 и размер. Ошибка, отмена, превышение лимита или несовпадение
байтов не дают допуска. Put проверяет тот же digest/size. Отчёт сохраняется
атомарно с immutable object receipt и артефактом либо prepared ledger.
Новые worker, endpoint и bearer grants для сканирования не создаются.

`artifact_revision_content` — единственный источник доказательства AV:
engine/database, policy, timestamp, verdict связаны с уже имеющимися exact
object key/version/ETag/digest/size. Это не отдельная конкурирующая модель
состояния. Исторический `CLEAN` без доказательства не даёт download/bind/runtime
materialization: публичные и привилегированные чтения используют одинаковую
эффективную eligibility-проекцию. История и байты не переписываются. Повторная
проверенная загрузка создаёт новый artifact обычным upload-путём: она не
снимает карантин прежнего artifact и не меняет его bindings. Нужные привязки
обновляются штатно владельцем.

## Жизненный цикл

| Переход | Владелец и результат |
| --- | --- |
| Новый upload | Preflight, bounded AV вне owner transaction, exact Put, свежая authority/OCC/idempotency и один commit receipt/audit/event |
| Повтор upload | Сначала текущая authority и idempotency receipt; повтор не выдаёт старый непроверенный CLEAN |
| Runtime complete | Scan/Put вне transaction; commit только при живой exact lease и разрешённом полном graph transition |
| Prepared content | Scan до durable stage; STAGED/adoption требуют persisted proof, exact object и прежние source pins |
| Отмена/expiry во время scan | Context закрывает соединение; owner recheck не допускает поздний эффект; существующий bounded cleanup убирает только собственный object |
| Retry | Новый действующий attempt/lease; исторический scan не назначает authority |
| Scanner unavailable/stale/limit | Закрытый отказ, не CLEAN; автоматического внешнего действия нет |
| Infection/формат запрещён | Upload остаётся QUARANTINED без bind/download; avatar/runtime/prepared content отклоняются |
| Delete/purge | Существующий owner lifecycle; AV proof удаляется вместе с exact content receipt, новый граф зависимостей не возникает |

## Выполнение

- [x] Потоковый AV-клиент с exact digest/size, timeout/cancel и проверкой базы.
- [x] Все upload/avatar/runtime/prepared producers и immutable evidence в коде.
- [x] Forward-only migration, единая eligibility, исторические UNKNOWN без допуска.
- [x] Clamd limits/AlertExceedsMax и приватный Unix transport в конфигурации.
- [ ] Unit и disposable PostgreSQL, негативные границы и безопасный EICAR.
- [ ] Scoped rollout, browser screenshot/Console/Network и журналы.

AV не доказывает отсутствие prompt injection. Семантический анализ HR-документов
и его отдельный серверный допуск не объявляются выполненными этой задачей.
Документы ClamAV проверены через Context7: официальный protocol INSTREAM и
`clamd.conf.sample`, включая AlertExceedsMax и AlertEncrypted.

## Порядок включения

1. Убедиться в отсутствии активных runtime executions и неподтверждённых
   отправок; сохранить текущие UID/source/image receipts и резервную копию.
2. `tools/deploy/artifact-scanner-release.mjs plan|apply|readback` создаёт новую
   immutable ConfigMap и меняет только scanner config/tmp в CP с двумя
   репликами. Прежняя ConfigMap не изменяется и не удаляется.
3. `tools/dev/probe-artifact-antivirus.mjs --context k3d-kodex` проверяет
   точную конфигурацию и безопасные clean/EICAR controls на обоих реальных
   экземплярах. EICAR не входит в письма, пользовательские файлы или агента.
4. Существующий `tools/release/control-plane-migration.mjs` с профилем
   `artifact-antivirus-admission` запускает exact forward migration штатным
   migrator и читает подтверждение Goose версии `20261016000100`.
5. Source-релиз CP через существующий hot-reload deployment tool. До завершения
   смешанного rollout старый producer не сможет записать файл без evidence:
   временный отказ загрузки безопаснее ложного CLEAN. Чтение платформы не
   требует отключать публичный HTTPS/SSO. Внешние письма не отправляются.
6. Реальная загрузка безопасного файла, history/download/bind, Console/Network
   и журналы; повторный readback двух экземпляров. Schema и AV eligibility не
   откатываются вниз; прежний код не должен обходить обязательный proof.

Все code-first этапы выполняются в существующем разрешённом локальном контуре.
До живого выполнения соответствующие пункты и EICAR остаются NOT RUN.
