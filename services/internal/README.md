# Внутренние сервисы

Сервис владеет доменными инвариантами и своим PostgreSQL source of truth.
Внешние клиенты обращаются к нему через gateway и versioned Proto/gRPC.

## Реализованные unit

- [`internal-rpc-authority`](internal-rpc-authority/README.md) — workload-local
  issuer/verifier, persistent replay boundary и lifecycle технических
  PostgreSQL credentials.
