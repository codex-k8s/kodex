---
id: GUIDE-MC-009
title: Герметичный и PostgreSQL-контуры Go
type: guide
status: approved
owner: developer
version: 1.0.0
updated: 2026-08-23
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
