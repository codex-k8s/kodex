---
id: OPS-MC-006
title: Развертывание и откат
type: operations
status: approved
owner: sre
version: 1.0.0
updated: 2026-08-23
---

# Развертывание и откат

## Release contract

Один release lock связывает exact Git SHA, профиль и immutable digest каждого
image. Render проверяет отсутствие placeholder, zero digest и mutable tag.
Порядок развертывания:

1. прямые stateful dependencies и security infrastructure;
2. fresh database migration и NATS stream bootstrap;
3. `control-plane` и локальный authority path;
4. runtime, scheduler и integration worker;
5. API gateway и Control Center;
6. optional interaction adapter выбранного профиля;
7. отдельный service-graph smoke.

Kubernetes readiness не используется как distributed dependency graph. При
недоступном соседнем сервисе рабочий запрос получает типизированный
`Unavailable` либо HTTP `502/503/504`, а Pod остаётся готовым, если его локальные
инварианты соблюдены.

## Откат приложения

Откат выполняется только на предыдущий полный release lock совместимого fresh
schema/runtime ABI. Нельзя смешивать images из разных lock. Перед откатом
проверяются:

- совместимость единственной baseline и текущей forward-only schema;
- RuntimeRevision и role-image ABI;
- NATS subject/event contract;
- authority policy/source revision;
- отсутствие активной destructive migration.

Database `down`, удаление PVC, очистка event store и ротация секретов не являются
частью обычного application rollback. Если schema уже несовместима с прежним
image, используется новый исправляющий release, а не ручное изменение БД.

## Fresh reset

Полный reset допустим только для новой установки по `runbooks/fresh-install.md`.
Он уничтожает данные и не является rollback. Агент не выполняет reset, merge или
deployment без отдельного подтверждения владельца.

## Evidence

Отчёт фиксирует profile, base/head SHA, release-lock digest, применённую schema,
результаты readiness и service-graph smoke, а также `PASS`, `FAIL`, `NOT RUN`.
Отсутствующий CI или live test не обозначается как успешный.
