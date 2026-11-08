---
id: OPS-AUTHORITY-PROOF-COUNTER-2062
title: Независимый устойчивый счётчик authority proofs
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-11-08
---

# Независимый счётчик authority proofs

Задача: [#2062](https://github.com/codex-k8s/kodex/issues/2062),
квалификация установки: [#1977](https://github.com/codex-k8s/kodex/issues/1977).
Изменяется один deployable unit — control-plane, включая CLI, migration,
schema/backup proofs и поддерживаемый forward upgrade.

Выдача proof и platform event раньше меняли разные колонки одной строки
`installation`. PostgreSQL SERIALIZABLE рассматривает изменение всей MVCC
строки, поэтому непрерывная выдача proofs исчерпывала ограниченные повторы
owner-транзакции. Миграция `20261108000100` переносит watermark в отдельную
`authority_proof_revision_counter`. Platform sequence, транзакции событий,
receipts, tenant/actor admission и пределы повторов не меняются.

## Полный путь и жизненный цикл

| Сценарий | Источник полномочий и effect | Результат / recovery |
| --- | --- | --- |
| Выдача proof | Прежний server-owned issuer → repository → один UPDATE counter | Уникальная возрастающая revision до JS-safe maximum; только после commit |
| Параллельная owner write | Прежние actor/tenant и exact owner command → SERIALIZABLE state/receipt/event/outbox | Независимый proof UPDATE не меняет installation snapshot; авторизация проверяется заново при прежнем bounded retry |
| Cancel / ошибка allocation | Caller context, прежний error mapping | Revision не выдаётся как успешная; потерянный ответ может оставить пропуск, но не разрешает уменьшить watermark |
| Исчерпание / отсутствующая строка | UPDATE-only allocator | Закрытый conflict; INSERT/reseed/fallback отсутствуют |
| Migration035→036 | Только мигратор; остановленные и joined старые issuers; точный idle predecessor | Один seed из committed legacy watermark под table lock; Goose повторно не выполняет seed |
| Старый issuer после036 | Legacy UPDATE колонки installation | Trigger отклоняет SQLSTATE55000; самостоятельная вторая последовательность не появляется |
| Backup / restore | Точный036 catalog и отдельная backup identity | Только SELECT нового counter; сохранение watermark обязательно; повтор ACL restore идемпотентен |
| Restart / delegation | Прежний защищённый issuer и durable verifier history | Тот же DB counter; Pod replacement не сбрасывает историю |

Отдельное доменное событие при allocation не создаётся: authoritative read path —
durable counter владельца, а выданный подписанный proof проходит прежние
проверки времени, issuer, method, tenant, generation и replay на consumer.
Общий invocation или произвольный payload не выбирает revision.

## Установка и переход

Новая установка выполняет полную цепочку Goose и exact036 normal proof.
Обновление использует закрытые `authority-proof-counter` либо
`authority-proof-counter-backup` immutable migration profiles, а portable
bootstrap регистрирует только035→036. Исторические035 verifiers не расширяются.
Generator проверяет SHA frozen035 и создаёт отдельные036 SQL/JS outputs.

До применения нужны проверенные immutable images с новым control-plane/CLI,
node-pull evidence, сохранённые preimage, полный release lock и резервная копия
БД с точным watermark. Предпочтительный путь — полный immutable release cohort
с новым schema head и штатным bootstrap-upgrade receipt. Существующий optional
component image profile требует неизменных schema heads и **не подходит** для
скрытого перехода035→036; его проверка не ослабляется.

Последовательность оператора:

1. Подготовить и проверить desired render, image/source/schema pins и оба узла.
2. Закрыть новые запуски; дождаться terminal/join runtime, builds/admissions,
   renewals и integration effects/probes. Сохранить backup/preimage.
3. Штатным maintenance stop остановить все старые control-plane issuers и
   подтвердить отсутствие старых claim sessions. План до stop может наблюдать
   такие sessions; непосредственный apply требует ноль и stopped workload.
4. Применить migration один раз immutable CLI. При неизвестном результате
   сначала inspect Goose history и exact036 catalog; create/DDL не повторять.
5. Проверить normal либо backup036 proof и сохранение watermark, затем
   запустить новый cohort. Проверить реальную выдачу proofs, owner write и
   admission verdict/report/event. Ready Pod не заменяет эту проверку.

Переход forward-only. Старый image после036 не является rollback: его allocator
закрыт trigger, а legacy watermark больше не меняется. `goose down` запрещён
самой миграцией. При отказе после commit сохраняется maintenance и выполняется
forward recovery новым проверенным image; нельзя просто вернуть старую БД,
если уже выданы новые proofs. Изолированное восстановление проверяет монотонность;
возврат всей установки из backup требует отдельного согласования с persistent
consumer replay histories. Эта задача не объявляет такой аварийный откат безопасным.

## Воспроизводимые проверки

Используются только disposable PostgreSQL18.3 и synthetic fixtures:

- `bash scripts/tests/control-plane-postgres-test.sh --authority-proof-counter-upgrade`;
- `bash scripts/tests/control-plane-postgres-test.sh --authority-proof-counter-fresh-install`;
- `bash scripts/tests/control-plane-postgres-test.sh --authority-proof-counter-owner-regression`;
- `make test-control-plane-postgres`, `make test-install-contract`,
  `make test-web-only-release` и изменённые Go tests/vet/build;
- `python3 tools/release/generate-authority-proof-counter-proof.py --check`;
- `node --test tools/release/control-plane-immutable-migration.test.mjs`.

Storage checks включают populated watermark, fresh zero, concurrent runtime
allocations, repeat Up, JS-safe exhaustion, legacy rejection, backup SELECT и
отказ восстановления при WRITE. Counter catalog отвергает лишние columns/indexes,
изменённые constraints/ACL, выключенный legacy trigger и отсутствующий watermark.
Отдельный native `pg_dump`/`pg_restore` переносит полную synthetic БД в новую БД
того же disposable контейнера: следующий runtime UPDATE обязан выдать revision
выше сохранённого watermark, а legacy allocator остаётся закрыт. Этот scoped
сценарий не подтверждает восстановление всей распределённой установки и её consumers.
Owner regression пересекает PROJECT/ORGANIZATION и manage/оба admission verdicts:
настоящая независимая allocation происходит после owner write до event commit.
Installed и disaster-recovery evidence фиксируются отдельно от локальных проверок.
