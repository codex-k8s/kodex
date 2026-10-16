---
id: OPS-ARTIFACT-ANTIVIRUS-1814
title: Серверный антивирусный допуск точных байтов файла
type: operation-plan
status: approved
owner: manager
version: 1.0.1
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
- [x] Unit и disposable PostgreSQL, негативные границы и безопасный EICAR.
- [x] Scoped rollout, browser screenshot/Console/Network и журналы.

AV не доказывает отсутствие prompt injection. Семантический анализ HR-документов
и его отдельный серверный допуск не объявляются выполненными этой задачей.
Документы ClamAV проверены через Context7: официальный protocol INSTREAM и
`clamd.conf.sample`, включая AlertExceedsMax и AlertEncrypted.

## Порядок включения

1. Убедиться в отсутствии активных runtime executions и неподтверждённых
   отправок; сохранить текущие UID/source/image receipts и резервную копию.
   `tools/deploy/control-plane-pre-migration-backup.mjs --context k3d-kodex
   --output-directory <private-directory>` сохраняет read-only custom dump
   Control Plane с SHA-256, exact PostgreSQL instance и проверкой TOC.
   Это не резервная копия всей платформы и не доказательство restore:
   S3 и другие домены не копируются; restore остаётся отдельным этапом плана.
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

## Проверка исходников и исправления

Исходный checkpoint `c469d3da85b24a6377fd4bf4104ad42b9449faa9`
опубликован в Draft PR#1815 поверх PR#1811. Полный Go unit Control Plane,
адресные disposable PostgreSQL upload/avatar/prepared/immutable/preview и
применимые regression, fresh/repeated migration и оба release render прошли
локально. Синтетический scanner в component fixture не выдаётся за live AV.

Независимая проверка выявила три проблемы старых файлов: ложную доступность
preview, невозможность снять старую KNOWLEDGE binding после карантина и
сохранение format-only CLEAN в receipt/event при иной aggregate version.
Исправления сохраняют raw immutable историю, но повторно вычисляют допуск
точной revision; снятие существующей binding не разрешает новый bind или bytes.
Частичная VFS проекция получает действия из авторитетного snapshot, не
выдавая неполную entity за revision. Проверка VFS включает положительный DOWNLOAD.
Исправленный SHA и результаты повторной проверки фиксируются после commit.

На установленном контуре backup-controller отсутствует. Перед AV migration
необходим описанный локальный CP dump; полный platform backup/restore остаётся
незавершённым пунктом общего MVP checklist, а не неявным PASS.

Checkpoint `0f654603360910834dc4bc80766cc4ef094d9e6a`: полный CP Go unit,
26 Node tests и адресный disposable PostgreSQL контур PASS. Product и
architecture повторно проверили этот SHA без P0/P1/P2. Живые rollout, AV,
browser upload и backup ещё NOT RUN. Отдельный запуск direct-run/VFS
первоначально FAIL: fixture зависел от ранее выполненного наблюдения каталога
модели. Добавлен явный штатный seed только в test fixture; повтор direct-run,
VFS DOWNLOAD/изоляция и concurrent-lease reads PASS на исправленном дереве.

## 16.10: включено и проверено

Развёрнутый исходник `1772fe822813d53c1c415370a53302be4737baf2`:
два CP и два API gateway готовы, монтируют один read-only checkout;
точный digest AV-кода сверён в обоих CP, restart count0. Clamd image
`docker.io/clamav/clamav@sha256:0e31ce089574268aefa0b543767d66b70240ab51ed49eec53e07f18d5629d817`,
движок1.5.4, база28154 от15.10.2026. Обязательная Goose migration
`20261016000100` подтверждена exact Job UID/source/version.

- PASS: read-only CP dump374798793 bytes, SHA-256
  `806265cc1faaef050b6b6beb7e6dac04ce6ac4470c39ef59cc955094dc997e60`;
  archive TOC прочитан. Full-platform backup/restore этим не подтверждается.
- PASS: clean и безопасный EICAR на обоих scanner Unix endpoints до миграции
  и повторно на обоих новых экземплярах после source rollout. EICAR не
  загружался в пользовательские artifacts, письма или runtime агента.
- PASS: fresh trusted-cluster render. Найден и исправлен stale assertion
  RBAC архиватора: ожидание приведено к каноническим более узким правам;
  реальные Role/manifests не расширялись.
- PASS: браузерная загрузка42 bytes, CLEAN; скачаны те же bytes; replay
  вернул тот же ref; история содержит одну revision. Безопасный HTML control
  остался QUARANTINED, download403. Снимок1440×1000 проверен визуально.
  После штатного входа ошибки JavaScript/MIME и неожиданные5xx отсутствуют;
  начальные401 и намеренный403 не скрыты и ожидаемы.
- PASS: отдельный реальный краткий модельный запрос через UI завершился
  SUCCEEDED; это не полный dogfooding workflow и не отправка письма.
- PASS: полный поддерживаемый disposable PostgreSQL contour164.141s,
  затем отдельный avatar contour1.744s, worker/runner readback. Предыдущий
  запуск основного contour157.314s прошёл, но wrapper FAIL на отсутствующих
  isolated-avatar test env; wrapper исправлен. Project-assistant fixture
  теперь явно подключает синтетический scanner. Production policy не менялась.

Последние изменения оснастки после развёрнутого SHA не меняют рабочий код.
Архитектурная, продуктовая и security проверки развёрнутого SHA — без
P0/P1/P2. Итоговая проверка финального commit фиксируется в том же PR#1815.
Полная HR semantic-admission цепочка, full backup/restore и итоговая ручная
приёмка MVP не объявляются выполненными этим журналом.
