---
id: GUIDE-MC-009
title: Герметичный и PostgreSQL-контуры Go
type: guide
status: approved
owner: developer
version: 1.0.3
updated: 2026-10-28
---

# Герметичный и PostgreSQL-контуры Go

Документ описывает фактически поддерживаемые точки входа web-first baseline.
Команда не получает доступ к staging/production DSN и не читает credentials
живой среды.

## Герметичный контур

```bash
make test-go
```

Цель выполняет:

1. проверку закреплённой версии Go и negative contract toolchain;
2. `make check-sql-boundary`, запрещающий SQL в Go-строках и проверяющий
   отдельные `sql/*.sql` с `//go:embed`;
3. `go test -tags= ./...` для каждого модуля под `libs/go` и `services` с
   `GOENV=off`, `GOWORK=off` и без внешнего `GOFLAGS`.

Герметичные unit-тесты не используют Docker, Kubernetes, внешние DSN,
Mattermost или provider credentials. Тест, которому нужна PostgreSQL, находится
в отдельном component-контуре и не маскирует отсутствие БД через `t.Skip`.

## Disposable PostgreSQL

```bash
make test-control-plane-postgres
```

Оснастка запускает закреплённый digest PostgreSQL 18 на случайном порту только
`127.0.0.1`, ждёт `pg_isready`, применяет fresh baseline migration, проверяет
`status`, повторный идемпотентный `up` и component suite control-plane. Контейнер
имеет уникальное имя и удаляется trap после любого исхода.

Component suite использует только созданный этой командой DSN и покрывает:

- fresh schema и повторный bootstrap;
- системного помощника и protected core prompt;
- создание проектов, агентов, workflows, runs, child delegation и callbacks;
- idempotency same-key/same-intent и конфликт same-key/different-intent;
- OCC, cancel/retry lineage и Human Gate one-winner;
- монотонную event sequence, outbox и audit;
- integration definitions, connections, grants и отсутствие обязательной
  внешней integration connection.

Если Docker или `pg_isready` отсутствуют, команда завершается ошибкой и в
отчёте классифицируется `NOT RUN`; это не считается `PASS`. Подключать к этой
цели внешний DSN запрещено.

## Ограниченная RAM-площадка

Для локального Linux Docker поддерживается отдельный ограниченный профиль
`KODEX_TEST_POSTGRES_STORAGE=ram` у той же PostgreSQL-оснастки. Он использует
только уже установленный закреплённый образ18.3 (`--pull never`), read-only root,
tmpfs512MiB UID/GID70/mode0700 для `/var/lib/postgresql`, два tmpfs16MiB для socket/tmp, hard1GiB без
swap и1CPU. До bootstrap проверяются actual cgroup, tmpfs, owner/mode и отсутствие bind/volume
mounts. Сверка не читает container env. Очистка использует положительный exact
container ID и подтверждает его отсутствие; неизвестный Create не повторяется.
Прежний disk-профиль остаётся default. Поддерживаются прежние bridge и host
режимы; host слушает только loopback, firewall не меняется.

Например, `KODEX_TEST_POSTGRES_STORAGE=ram KODEX_TEST_POSTGRES_NETWORK=host
KODEX_CONTROL_PLANE_TEST_GO_PARALLELISM=1 make test-workspace-volume-predecessor-postgres`
выполняет настоящий010 predecessor proof, переходы011…017 и SSH-проверки
в одноразовой RAM-БД. Отдельный release profile `workspace-volume-ssh`
проверяет полный actual graph109/303, точный каталог custody017 и сохранение
полной inverse014-проекции с единственной exact DEPLOYMENT-веткой. Исторические
verifier015/016/SSH не принимают новый профиль. Counterfactual-проверки изменяют
columns, constraints, indexes, table/column/function ACL, function body/owner,
RLS/policies, internal parent-side FK и origin-close triggers; отдельно проверяются
изменения прежнего evidence guard и новой DEPLOYMENT-ветки. Все изменения
выполняются с rollback в disposable БД. Каталоговые PostgreSQL API проверены
через Context7 `/websites/postgresql` (`pg_get_triggerdef`, `pg_get_expr`,
`REPEATABLE READ`). Go build cache и
временные файлы настраиваются вызывающим процессом отдельно. Это component/
schema qualification, не staging capacity admission, backup durability либо
сквозная приёмка. Семантика Docker CLI проверена через Context7
`/docker/docs`; путь data root сверён с [официальным описанием образа PostgreSQL](https://github.com/docker-library/docs/blob/master/postgres/README.md#pgdata)
и конфигурацией закреплённого локального образа. Результат относится к фактически
исполненному SHA; наличие профиля не считается PASS.

`make test-workspace-volume-owner-postgres` в той же оснастке сначала выполняет
полную native016→017→SSH schema qualification, затем создаёт отдельный template
для writer component. Сценарий через штатные domain commands проводит pending
CREATE/PREPARE, claim replay, renew/COMPLETE и ACK replay до допуска model lease
и BIND. Завершённый physical grant не продлевается; до accepted preparation
model claim пуст. Physical Job/Pod/PVC observations заданы явными fixtures, без
настоящего Kubernetes, модели и object store. Старые release-verifiers не
расширяются: этот контур не заменяет combined staging migration proof и полный
web-first baseline. Бюджет цели240s, включая схему и component; кэш предварительно
собирается отдельно при необходимости.

## Каталог checkpoint рабочего дерева

`make test-runtime-checkpoint-predecessor-postgres` использует ту же закрытую
одноразовую upgrade-БД с общим пределом240s. Применяется полная схема015,
проверяется точный архивный каталог, затем выполняется настоящая016 и два
повторных обновления. Тринадцать изменений колонок, defaults, ACL, RLS,
индексов, publication key, trigger function и FK enforcement проверяются с
обеих сторон миграции. Каждое изменение происходит в отдельной откатываемой
транзакции. Бизнес-строки, staging credentials и внешние эффекты не нужны.
Этот контур доказывает границу схемы; он не заменяет owner lifecycle,
физическое сохранение PVC/объектов, immutable rollout или пользовательский PDLC.

## Серверный DAG

```bash
make test-workflow-server-postgres
```

Отдельная цель #1836 запускает десять составных проверок через ту же одноразовую
PostgreSQL-оснастку, с общим пределом 240 секунд и десятью секундами на остановку.
На Linux-хосте с закрытым Docker DNAT поддерживается
`KODEX_TEST_POSTGRES_NETWORK=host`: БД слушает только loopback.
Общий `test-control-plane-postgres` сохраняет прежние сценарии и добавляет
изоляцию счётчиков обзора и проверку текущей схемы010. Исторические release
verifiers002 не расширяются: новый профиль отдельно проверяет workflow,
права purge и локальные ограничения repository bundle. Native schema fixtures
проверяют также отказ при отключении guards, удалении CHECK/FK/index и выдаче
PUBLIC права EXECUTE; изменения происходят только в отдельных копиях БД.

Проверяются захват и изоляция файлов, промежуточный `CAPTURE_PROGRESS`,
передача принятого результата двум следующим сотрудникам, история графа и
событий, повтор callback, разбор ошибки менеджером, четыре бизнес-попытки,
параллельные отказы, продолжение разрешённого шага при открытом Human Gate,
истечение gate и удаление проекта через штатные команды. Обратная связь
владельца и менеджера проверяется при повторе завершённого шага и после
исправления конфигурации, которая не позволила начать первую попытку. Отдельная проверка
savepoint отличает отсутствие конфигурации от ошибки БД. Проверка rejoin покрывает
повторное использование подтверждённого внешнего результата после отклонения
бизнес-результата, отзыв grant, новые ссылки на исходные файлы, новое поколение
lease на прежнем узле и несколько продолжений одной попытки. Счётчики
invocation и receipt доказывают отсутствие повторного WRITE в этой оснастке;
она не выполняет настоящий запрос GitHub и не заменяет сквозную проверку.
Отдельный сценарий с двумя открытыми Gate проверяет продление и истечение
lease: независимый шаг продолжает работу, а новый claim шага с собственным
Gate разрешается лишь после закрытия обоих согласований.

Успех этой цели подтверждает работу доменных команд на PostgreSQL.
Реальные модели, GitHub, SSH-развёртывание, антивирус, браузер и полный PDLC
проверяются отдельно; эта цель не подтверждает готовность staging или релиза.

## SQL boundary

Каждый production query хранится отдельным файлом
`internal/repository/postgres/<capability>/sql/<name>.sql` и загружается в
именованную строковую константу через `//go:embed`. В Go запрещены SQL literals,
конкатенация query text и ручное чтение `.sql` в runtime.

Новый или изменённый query начинает файл с `-- name: <stable_name>`, использует
typed аргументы `pgx.NamedArgs` и не смешивает несколько независимых запросов в
одном файле. Runtime error не включает SQL text, DSN или аргументы.

## Результаты

- `PASS` — точная команда завершилась успешно;
- `FAIL` — test assertion, build, migration или оснастка завершились ошибкой;
- `NOT RUN` — обязательный безопасный внешний инструмент недоступен и сама
  команда не могла быть запущена.

Локальный результат относится к текущему SHA рабочего дерева и отдельно
указывается в PR. Он не называется GitHub CI без фактического check run.

Связанные документы: `GOV-DOC-003`, `GO-DOC-001`, `GO-DOC-002`,
`GUIDE-DOC-003`, `GUIDE-DOC-006`.
