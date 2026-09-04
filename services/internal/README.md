---
id: REPO-MC-002
title: Внутренние сервисы
type: repository-readme
status: approved
owner: backend
version: 1.0.0
updated: 2026-08-23
---

# Внутренние сервисы

Сервис владеет доменными инвариантами и своим единым источником истины в
PostgreSQL. Внешние клиенты обращаются к нему через шлюз и версионированный
Proto/gRPC.

## Реализованные компоненты

- [`internal-rpc-authority`](internal-rpc-authority/README.md) — локальные для
  workload issuer/verifier, устойчивая граница защиты от повтора и жизненный
  цикл технических учётных данных PostgreSQL.
- [`control-plane`](control-plane/README.md) — авторитетные проекты, роли,
  конфигурация, sessions/run lineage, schedules, gates, integration metadata и
  artifacts.
- [`secret-broker`](secret-broker/README.md) — единственная plaintext boundary
  Runtime Secrets и producer exact runtime/STT credential projections.
- [`stt-tts-service`](stt-tts-service/README.md) — неактивный base deployable
  stateless STT; до материализации #1021/#1023 не входит в shipped profiles и
  закрыто отказывает до projection RPC.
