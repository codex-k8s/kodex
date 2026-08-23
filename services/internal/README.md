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
