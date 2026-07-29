---
id: ARCH-DOC-002
title: Технологический стек
type: architecture
status: approved
owner: architect
version: 1.0.1
updated: 2026-07-28
---

# Технологический стек

## Базовый профиль

| Область         | Технология             | Правило                                                          |
| --------------- | ---------------------- | ---------------------------------------------------------------- |
| Backend         | Go                     | отдельный module на deployable, типизированная конфигурация      |
| PWA             | Vue + TypeScript       | composition API, generated clients, без ручного дублирования DTO |
| Внешний API     | OpenAPI/REST           | публикуется gateway, не внутренним сервисом                      |
| Внутренний API  | Proto/gRPC             | generated clients, deadline и mTLS                               |
| События         | AsyncAPI               | владелец состояния публикует через outbox                        |
| Realtime        | WebSockets             | контракт в AsyncAPI, аутентификация через gateway                |
| Source of truth | PostgreSQL             | forward-only goose, named SQL                                    |
| Cache           | Redis                  | TTL protobuf snapshot, не источник истины                        |
| Broker          | NATS JetStream         | broker-neutral relay/inbox API                                   |
| Object storage  | S3-compatible          | controlled upload и immutable object identity                    |
| Runtime         | Kubernetes             | Kustomize base + environment overlays                            |
| Secrets         | Vault                  | namespace/workload-bound delivery                                |
| Identity        | Keycloak/OIDC          | внешняя identity не заменяет доменную authorization              |
| Metrics         | Prometheus + Grafana   | закрытая кардинальность labels                                   |
| Traces          | OpenTelemetry + Jaeger | OTLP/gRPC                                                        |
| Errors          | Sentry                 | только unexpected errors, без PII                                |

Замена технологии допустима ADR, если сохраняются функциональные и
эксплуатационные инварианты.

Нормативные профили реализации: `GO-DOC-001` для Go-сервиса,
`GO-DOC-005` для gRPC/NATS и `GO-DOC-006` для общих библиотек.
