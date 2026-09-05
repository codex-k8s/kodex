---
id: OPS-EMAIL-HTTP-1045
title: HTTP-квитанции почтовых операций
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-05
---

# Контракт

Источники: #1018, #1037, #1045, #1046, MVP-UI-42;
`OPS-EMAIL-CONTRACT-1046`, producer checkpoint `d31cd4c70`.
Это HTTP consumer контракт, не подтверждение готовности почтового сервиса.

| Сценарий | HTTP / SDK | CP RPC / operation | Authority, состояние и результат |
| --- | --- | --- | --- |
| Просмотр исхода | GET `/api/v1/integration-invocations/{invocationRef}/email-effect-receipt`, `getEmailEffectReceipt` | `GetEmailEffectReceipt`, `platform.query.email-effect-receipts.get` | Проверенная browser session; CP выводит tenant/project и видимость invocation из owner state. Возвращает receipt и optional decision; ETag = receipt.version. Чтение не меняет состояние и не выдаёт grant. |
| Решение владельца | POST `/api/v1/email-effect-receipts/{receiptRef}/reconciliation`, `reconcileEmailEffect` | `ReconcileEmailEffect`, `platform.command.email-effects.reconcile` | Session, CSRF, signed context; CP проверяет свежую авторизацию и exact permission, затем owner и OCC. If-Match = receipt.version, Idempotency-Key обязателен. Возвращает отдельное решение; ETag = decision.version. |

Body решения содержит `expectedReceiptDigest` (нижний регистр SHA256 без
префикса), `outcome` (`EFFECT_CONFIRMED|NO_EFFECT_CONFIRMED`) и необязательный
`note` до 2000 Unicode-символов без NUL. Digest берётся из
`receipt.externalReceiptDigest`; actor/project/grant в body запрещены.
Ни path, ни digest, ни заголовок project не предоставляют полномочий.

Owner сохраняет прежнюю квитанцию неопределённого исхода для аудита. Команда
сама не отправляет письмо повторно. `NO_EFFECT_CONFIRMED` не является
произвольным разрешением retry: последующий email consumer обязан заново
получить exact server grant. Состояние, audit и idempotency сохраняет CP
атомарно; новый domain event этим контрактом не вводится. Авторитетный read
path приведён в таблице.

В UI передаются только refs, версии, digests, outcome и временные отметки.
Внешний effect key, external receipt ref и worker grant не сериализуются;
тело письма, адресаты и credential отсутствуют. Gateway отвергает неизвестные
enum, небезопасные JSON integers, неверные timestamp, несогласованные
receipt/decision refs, версии и digests. Историческое истечение decision не
скрывает результат аудита и не восстанавливает его разрешение.

RPC ошибки проходят общий безопасный Problem mapping. Свежая авторизация
передаётся как `FRESH_AUTHENTICATION_REQUIRED` только при доверенном
`ErrorInfo.domain=kodex.control-plane`; детали upstream не раскрываются.
Никаких HTTP путей для worker `ResolveEmailAuthorization`,
`ReportEmailEffectReceipt` или `ResolveEmailReconciliation` не добавлено.

# Проверки и ограничения

`TestEmailEffect*` проверяет реальные generated HTTP routes на fake gRPC client:
mapping, ETag, no-store, скрытие worker полей, входные ограничения и ошибки
authority/OCC, повреждённые ответы владельца. Общий тест security boundary
проверяет session, CSRF, revocation и несовпадение organization на обоих paths.
App test связывает оба RPC с exact authority profile без обязательного
project header.

Документы oapi-codegen получены через Context7:
[официальный Go package](https://pkg.go.dev/github.com/oapi-codegen/oapi-codegen/v2).
Генерация сама не заменяет проверку ограничений и ответов; используется
существующий строгий JSON decoder и явные typed преобразования.

На producer checkpoint `d31cd4c70` SQL/domain receipt handlers, fresh-owner
проверка и worker trust ещё не были реализованы. HTTP тесты с fake RPC не
доказывают эти части. Их итоговая реализация и защищённый сквозной тест остаются
обязательными зависимостями #1046/#1037 до приёмки. Live mail, новые сетевые
порты и staging этим изменением не запускались.
