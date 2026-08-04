# Внешние gateway

Gateway владеет внешним transport, аутентификацией границы, mapping и rate
limits, но не читает чужую БД и не реализует чужую бизнес-логику.

- [`control-api-gateway`](control-api-gateway/README.md) — owner REST/WebSocket
  boundary к авторитетному `control-plane`;
- [`integration-gateway`](integration-gateway/README.md) — MCP/provider
  integration boundary.
