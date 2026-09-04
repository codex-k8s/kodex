---
id: OPS-EMAIL-1037
title: Email bridge и границы интеграции
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-04
---

# Сценарии #1037

| Сценарий | Actor и authority | HTTPS/owner | Idempotency, state и effect | Readiness/read |
| --- | --- | --- | --- | --- |
| YAML/UI | Проверенный управляющий actor у #1046 | Configuration email-bridge/v1 → immutable control-plane revision → read-only projection | managed_by/source/revision; bridge не редактирует конфигурацию | Строгий разбор, persisted revision watermark |
| POP read/search/download | invocation token, mTLS integration-gateway; online resolve у #1046 | /v1/mailbox-operations → ResolveEmailAuthorization → POP | user∩agent∩connection∩resource; UIDL, bounded scan; без внешней mutation | Авторизованные protocol auth/NOOP и UIDL; POP не имеет folders/read flags |
| SMTP send/reply/reply_all/forward | Те же scopes плюс exact recipients/from и gate | ResolveEmailAuthorization до credential projection; bridge владеет receipt | reserve unknown до DATA; accepted только после final 250; timeout/crash остаётся unknown | receipt GET; accepted не означает delivered |
| POP delete | Exact UIDL и policy/gate | DELE + QUIT, owner receipt bridge | reserve unknown до DELE; deleted только после успешного QUIT | GET receipt; unknown не повторяется |
| Gate reject/pending | Owner #1046 | resolve возвращает отказ или gate_approved=false | Нулевой credential/protocol effect | Новый owner-approved grant, тот же immutable input |
| Revocation/cancel | Owner #1046 закрывает grant/credential | Каждый вызов resolve свежий, кэша authority нет | До protocol effect проверяются поколения descriptors | Unavailable/unknown state fail closed |
| Retry/crash/duplicate | Тот же tenant/mailbox/effect | Атомарная reserve в PostgreSQL | Один победитель; mismatch 409; unknown никогда не READY | Устойчивый read, без фоновой повторной отправки |

События bridge не публикует: source of truth receipt PostgreSQL и авторитетный
HTTPS read. Run/turn/gate lifecycle остаётся у control-plane. Отмена входящего
запроса закрывает protocol connection, но не стирает предварительно записанный
unknown. Автоматического SMTP reconciliation не существует: SMTP не предоставляет
поиск по idempotency key. Владелец сверяет серверные журналы/Message-ID вне bridge;
повтор того же key не отправляет письмо. Новое намерение требует отдельного
grant/effect после решения владельца, прежняя receipt не переписывается.

## Контракты для root

- #1046: `ResolveEmailAuthorization`, HTTPS POST
  `/internal/v1/email-authorizations/resolve`, models
  `AuthorizationRequest/AuthorizationDecision` из `libs/go/emailbridgeapi`.
  Producer подтверждает живые actor/agent/tenant/connection/grant, exact input
  digest, effect, config revision и credential generation; четыре scopes и gate
  берутся из authoritative state, не из запроса пользователя.
- #1045/#1022: `Configuration`, `Mailbox`, `Endpoint`, `OperationPolicy`, `Limits`
  в том же OpenAPI. UI и YAML используют одну schema, raw secret values отсутствуют.
- #1028/root integration: bearer является opaque invocation token для online
  owner resolve. Статический token подключения не заменяет разрешение операции.
  Старые send/status paths сохранены; полный API — `ExecuteMailboxOperation`.
- #1029: bridge использует только CONNECT к настроенному egress. Implicit TLS
  посылает ClientHello первым; STARTTLS требует отдельного mail-aware допуска
  SMTP greeting/EHLO/STARTTLS и POP greeting/STLS. Пока #1029 его не поддерживает,
  STARTTLS не готов, прямого dial fallback нет.

## Протокольные ограничения

POP3 имеет один maildrop, отображаемый как INBOX; discovery не выдаёт вымышленных
папок. `mark` возвращает UNSUPPORTED, чтение не меняет server-side flags.
UIDL обязателен. Search выполняется локально по bounded headers; курсор закрепляет
UIDL snapshot и filter, изменение mailbox требует нового поиска. RETR может
передать всё письмо: limits проверяются и по LIST, и по фактическим байтам.

Проверены Context7 `/emersion/go-smtp` (TLS, AUTH, DATA/final response и deadlines),
официальный [go-pop3](https://github.com/knadh/go-pop3),
[RFC1939](https://www.rfc-editor.org/rfc/rfc1939),
[RFC2595](https://www.rfc-editor.org/rfc/rfc2595),
[RFC5321](https://www.rfc-editor.org/rfc/rfc5321),
[RFC8314](https://www.rfc-editor.org/rfc/rfc8314).
