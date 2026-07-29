# Общие Go-библиотеки

Нормативный каталог, критерии вынесения, зависимости и failure policy задает
`GO-DOC-006` (`docs/guides/shared-go-libraries.md`).

Общий код выносится сюда после второго реального потребителя либо сразу по
утвержденному решению об обязательном общем runtime/security/data primitive.

Каждая библиотека:

- имеет отдельный Go module;
- предоставляет узкий provider-neutral API;
- не импортирует `internal` сервиса;
- не скрывает lifecycle и `context.Background()`;
- не логирует ошибку ниже transport/job boundary;
- имеет README с guarantees и failure policy.

Базовые модули:

- `serviceruntime`;
- `grpcserver`;
- `httpserver`;
- `observability`;
- `cache`;
- `eventing`, включая PostgreSQL outbox/inbox и NATS JetStream adapter;
- `internalrpcauth`;
- `sqlquery`;
- `securetoken`;
- `postgrescredential`.

Бизнесовые entities, repository ports, event payloads, permissions, cache keys,
TTL и SQL остаются в сервисе.
