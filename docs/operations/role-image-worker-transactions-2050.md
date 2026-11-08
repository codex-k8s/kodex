---
id: OPS-DOC-2050
title: Ограниченный повтор транзакций worker сборки RoleImage
type: runbook
status: approved
owner: sre
version: 1.0.0
updated: 2026-11-08
---

# Транзакции worker сборки RoleImage

Источник: [#2050](https://github.com/codex-k8s/kodex/issues/2050), квалификация
переносимой установки [#1977](https://github.com/codex-k8s/kodex/issues/1977).
Изменяется один deployable: `control-plane`. Wire-контракты, схема БД, runtime
конфигурация, admission policy и внешний BuildKit остаются прежними.

## Граница повтора

Проверенный workload `role-image-builder` вызывает специализированный RPC через
существующий generated adapter. Доменный сервис проверяет principal и передаёт
команду PostgreSQL-владельцу состояния. В каждой попытке заново разрешаются
actor/tenant/permission и блокируется точная строка сборки. Receipt читается до
проверки живой lease: только исходный intent может вернуть уже сохранённый ответ.
Для нового эффекта обязательны прежние version, attempt, fence, credential
revision, lease token и допустимое состояние сборки.

Повторяется целая транзакция после завершённого rollback, максимум три попытки
в исходном caller context с отменяемыми ожиданиями5/10ms. Driver SQLSTATE40001,
40P01 и известный abort классифицируются до безопасного отображения ошибки.
Обычный `Unavailable`, ошибка cleanup, version mismatch, отозванные полномочия
или неправильная lease не разрешают новый повтор. Ошибки драйвера, SQL и значения
токенов не входят в результат.

Для неизвестного результата Commit используется уже существующий role-image
marker: новая транзакция с тем же scope/key/intent получает точную сохранённую
receipt либо выполняет единственный ещё не совершённый эффект. Это допустимо
именно внутри данного owner path, где нет внешней сборки или push. Принцип не
разрешает автоматически повторять внешнее действие либо другой произвольный
commit. Ни одна промежуточная build/artifact/token пара не выдаётся при ошибке.

| Команда / исход | Атомарный результат и потребитель |
| --- | --- |
| Claim | Прежний механизм claim/retry; attempt/fence и его receipt не меняются этим исправлением |
| Renew | Одна новая version, token/expiry и receipt; отдельного build event нет; worker получает точный claim |
| Progress | Одна новая version/stage/percent, receipt и `ROLE_IMAGE_RECIPE_CHANGED`; outbox обновляет проекцию |
| Complete | Terminal build без активной lease, один artifact, audit, парный receipt и событие; admission видит только committed artifact |
| Fail | Прежнее failure state, очистка lease, один receipt и событие; дальнейшая обработка остаётся у владельца lifecycle |
| Точный replay после потери ответа | Исходный ответ без новой version, artifact, audit, события или продления lease |
| Неподходящие authority/OCC/token/intent | Закрытый отказ без повторной записи и без события |
| Исчерпание бюджета / cancellation | `Unavailable`, нулевой результат, отсутствие частичных committed записей |
| Cancel / expiry / retry / admission / promotion | Прежние специализированные owner transitions; исправление не вызывает их автоматически и не открывает gate |

## Воспроизводимая локальная проверка

Использовать чистое окружение без credentials внешних систем. Каноническая
команда создаёт отдельный loopback PostgreSQL из закреплённого образа, применяет
миграции и удаляет только собственный контейнер и случайные тестовые БД:

```bash
bash scripts/tests/control-plane-postgres-test.sh \
  '^Test(OrganizationRoleImages|RoleImage.*)Component$'
```

`TestRoleImageWorkerTransactionRetryComponent` подставляет40001/40P01 после
owner update: на записи platform sequence после receipt, а для renew без события
— на receipt insert. Нет прямой подстановки business state. Нетронутая
нетранзакционная sequence считает реальные попытки, а readback проверяет один
committed owner effect. Complete дополнительно требует единственный artifact.
Каждая команда повторяется с новым transport correlation и прежним intent:
response и persisted counts должны в точности совпасть.

`TestRoleImageWorkerTransactionRetryBoundsComponent` проверяет постоянную
ошибку23505, исчерпание трёх попыток, stale version/attempt/fence и неправильный
token. Отрицательные lease cases не достигают fault trigger. Unit проверки
`TestRoleImageWorkerRetryClassification` и `TestRoleImageWorkerRetryCancellation`
покрывают закрытую классификацию и отмену до первой/следующей транзакции.
`TestRoleImageBuildFreshCorrelationComponent` сохраняет отдельные отрицательные
проверки actor, workload, credential revision и tenant для четырёх команд.

## Установленная приёмка

Локальная fault injection доказывает исправление owner transaction, но не
устанавливает причину любого отказа работающего builder. Для принятого immutable
image записать source SHA/digest и schema proof, затем выполнить обычный
synthetic build → inventory → admission → promotion. Проверить terminal version,
один artifact и отсутствие повторного внешнего действия. Сохранить отдельный
результат каждого gate; зелёный Pod не заменяет эту приёмку.

Миграций нет. Возврат к предыдущему совместимому image возможен штатным
deployment-процессом после завершения активных сборок. Он вернёт прежнее поведение
повторов, но не требует отката БД или удаления существующих receipts/artifacts.
