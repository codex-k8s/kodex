---
id: GUIDE-DOC-005
title: Ошибки внутренних RPC и внешних HTTP API
type: guide
status: approved
owner: architect
version: 1.0.0
updated: 2026-07-28
---

# Ошибки внутренних RPC и внешних HTTP API

Внутренние сервисы возвращают `google.rpc.Status` с типизированным
`ErrorDetail`.
Свободный текст не является машинным контрактом и не передается пользователю
без безопасной локализации gateway.

| ErrorReason             | gRPC code          | HTTP status | Problem code            | retryable |
| ----------------------- | ------------------ | ----------- | ----------------------- | --------- |
| INVALID_REQUEST         | InvalidArgument    | 400         | INVALID_REQUEST         | false     |
| INVALID_REQUEST         | InvalidArgument    | 400         | DEADLINE_REQUIRED       | false     |
| UNAUTHENTICATED         | Unauthenticated    | 401         | UNAUTHENTICATED         | false     |
| PERMISSION_DENIED       | PermissionDenied   | 403         | PERMISSION_DENIED       | false     |
| NOT_FOUND               | NotFound           | 404         | NOT_FOUND               | false     |
| STATE_CONFLICT          | FailedPrecondition | 409         | STATE_CONFLICT          | false     |
| STATE_CONFLICT          | FailedPrecondition | 409         | `<DOMAIN_CONFLICT>`     | false     |
| IDEMPOTENCY_CONFLICT    | AlreadyExists      | 409         | IDEMPOTENCY_CONFLICT    | false     |
| RESOURCE_ALREADY_EXISTS | AlreadyExists      | 409         | RESOURCE_ALREADY_EXISTS | false     |
| VERSION_MISMATCH        | Aborted            | 412         | VERSION_MISMATCH        | true      |
| RATE_LIMITED            | ResourceExhausted  | 429         | RATE_LIMITED            | true      |
| INVALID_REQUEST         | ResourceExhausted  | 413         | PAYLOAD_TOO_LARGE       | false     |
| UNAVAILABLE             | DeadlineExceeded   | 504         | DEADLINE_EXCEEDED       | true      |
| INVALID_REQUEST         | Cancelled          | 499         | CANCELLED               | false     |
| UNAVAILABLE             | Unavailable        | 503         | UNAVAILABLE             | true      |
| INTERNAL                | Internal           | 500         | INTERNAL                | false     |

Набор внешних problem codes закрыт схемой `Problem` в owner OpenAPI и единым
mapper `control-api-gateway`. Gateway не передаёт пользователю свободный gRPC
текст: он преобразует только известный canonical gRPC code, а frontend
локализует стабильный `Problem.code` по текущей локали. Конфликт оптимистичной
версии передается как `VERSION_MISMATCH`.

Неизвестный gRPC code, отсутствующий detail, неизвестный `code` или
несогласованная пара `reason/code/retryable` преобразуется в `500 INTERNAL` и
регистрируется как нарушение внутреннего контракта. Внешний `Problem` не
содержит исходный текст, стек, идентификаторы организации или данные
зависимости.
