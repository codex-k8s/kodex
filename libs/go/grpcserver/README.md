# `grpcserver`

Общая gRPC error/recovery boundary MatterCodex. Она содержит единственный
предикат unexpected codes и ровно один error-observer вызов на transport
boundary. Service-specific domain mapping остаётся в сервисе.
