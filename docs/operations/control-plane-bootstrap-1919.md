---
id: OPS-BOOTSTRAP-1919
title: Приватная причина bootstrap и ограниченное восстановление транзакции
type: operation
status: approved
owner: developer
version: 1.0.0
updated: 2026-10-26
---

# Bootstrap control-plane: причина и повтор

Связано с #1919 и #1830. Штатный startup после initial PostgreSQL Ping выполняет
Bootstrap до открытия listeners/workers. Живое наблюдение одного exit1 с
`stage="bootstrap platform" error_class=unknown` не содержит SQLSTATE;
историческая причина остаётся UNKNOWN. Миграции #1916/#1918 и успешный последующий
purge сами по себе не доказывают причину рестарта.

В одноразовом PostgreSQL подтверждён отдельный причинный путь: первая реплика
удерживает installation lock, вторая начинает SERIALIZABLE snapshot и фактически
ждёт тот же lock. Первая сверяет integration definitions и commit. Вторая
после ожидания пытается блокировать изменённую после snapshot definition и
получает 40001. Это native доказательство, а не установление живого SQLSTATE.

## Матрица

| Переход | Авторитетный исход и cleanup | Повтор |
|---|---|---|
| Caller отменён до попытки | Без Begin и эффектов | Нет |
| Ошибка Begin | Сохранённая private cause | Нет |
| Отказ Query/Scan/Exec | Отмена всей owner transaction независимым bounded cleanup | Только 40001/40P01 после подтверждённого rollback |
| 40001/40P01 на Commit | Типизированный ответ PostgreSQL доказывает server abort | Разрешён в прежнем budget |
| Unknown commit/transport failure | Без выдуманного успеха; прежняя ошибка сохраняется | Нет |
| Rollback failure либо неподтверждённый closed до Commit | Закрытый отказ cleanup | Нет |
| Permission/auth/integrity/semantic failure | Закрытый отказ; policy/pins не исправляются автоматически | Нет |
| Успех | Один commit, затем продолжение startup barrier | Не требуется |
| Отмена/исчерпание startup budget | Нет новой попытки, приватная причина отмены сохраняется | Нет |

Используется существующая policy: максимум 3 попытки и bounded pause между ними.
Каждая попытка заново начинает SERIALIZABLE transaction и читает все прежние
owner/policy/credential pins. Общий StartupTimeout не меняется. Cleanup имеет
независимый предел 1 секунда от неотменённой основы caller context; это разрешает
лишь rollback, не продлевает допуск нового bootstrap.

Локальный transaction observer Bootstrap сохраняет driver cause даже если общий helper
свёл ошибку к domain sentinel. Он не сохраняет SQL text/arguments и не изменяет
statements, authority либо lifecycle других owner commands. Error остаётся
фиксированной фазой; process output использует прежний закрытый stage/class.
Нет новых сетевых правил, credentials, ролей БД, миграций и внешних эффектов.

## Проверка

Поддерживаемый disposable entrypoint:

```bash
bash scripts/tests/control-plane-postgres-test.sh '^TestConcurrentBootstrapComponent$'
```

Использовать канонический Go 1.26.6, GOMAXPROCS=2, GOENV=off/GOWORK=off,
штатный loopback Docker fixture и очищенное окружение без внешних DSN/credentials.
Fixture проверяет исходный отказ единственной попытки с40001, bounded whole-Tx retry и отмену настоящего
lock wait. QueryEnd barrier и exact backendPID/pg_blocking_pids доказывают порядок;
readback проверяет отсутствие лишних owner/effect revisions и точные изменения
версий definitions только committed попытками. Все goroutines имеют cancel/join,
точный контейнер удаляется после любого исхода.

Unit проверки покрывают сохранение private cause/безопасный reporter, permission,
unknown commit после прежнего 40001, failed rollback, cancellation и предельное
число попыток. Неподдерживаемые native внешние paths и старые остановленные сценарии не
запускаются этой командой. Native/test PASS не заменяет независимое review и
owner staging acceptance.

## Доставка и возврат

Схема и контракты передачи не меняются. Поставка только адресного immutable CP image
штатным оператором релиза после review. При ручной проверке без активных задач
сверить source/image/replicas, безопасные startup fields и сохранность history/files;
не отключать PostgreSQL либо соседние сервисы ради воспроизведения.
Возврат прежнего образа не меняет сохранённый owner state, но возвращает прежнюю
startup диагностику и отсутствие bounded bootstrap retry. Отсутствие нового рестарта
не превращает историческую причину в установленную.

## Документация зависимостей

Через Context7 проверены pgx v5.10.0 (transaction cleanup, deferred Rollback,
ошибки закрытой transaction) и PostgreSQL (SERIALIZABLE snapshot/locking).
Официальные источники: [pgx transactions](https://github.com/jackc/pgx/blob/v5.10.0/tx.go),
[PostgreSQL isolation](https://www.postgresql.org/docs/18/transaction-iso.html),
[PostgreSQL locking](https://www.postgresql.org/docs/18/explicit-locking.html).
Эти гарантии объясняют disposable regression, но не устанавливают причину
исторического живого отказа.
