---
id: OPS-EMAIL-HTTP-1045
title: HTTP-квитанции почтовых операций
type: operations
status: approved
owner: developer
version: 1.1.0
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

# Одноразовое подтверждение

Источники: #1045/#1046, `GUIDE-DOC-003`, `GUIDE-DOC-006`, policy
`10132529a`. Подтверждение ограничивает действие в gateway, не заменяя owner
permission в CP. OIDC signature/issuer/audience/auth_time/ACR/AMR проверяет
существующий verifier; actor/tenant/sid/revision происходят только из него.

| Переход | Проверки и владелец | Результат / отказ |
| --- | --- | --- |
| Выдача | POST session с purpose EMAIL_EFFECT_RECONCILIATION, receiptRef/receiptVersion/receiptDigest; OIDC auth_time не старше 5 минут, непустые проверенные ACR/AMR | Шифрованная browser session с exact receipt binding; expiry не позднее min(auth_time+5m, now+2m, bearer expiry). Session fields не назначают project либо permissions. |
| Использование | POST reconciliation; session/Origin/CSRF/revocation; body digest и If-Match совпадают с purpose, receiptRef совпадает с path | Durable ConsumeOnce по browser session ID до business RPC, замена cookie обычной session, затем CP повторно проверяет свежесть, owner, permissions и OCC. |
| Повтор / гонка | Две реплики обращаются к одному существующему durable revocation store | Только один победитель может вызвать CP; повтор требует нового подтверждения. |
| Неверная квитанция | Ref/version/digest либо kind не совпали, secret/project fields в email purpose | Закрытый отказ до ConsumeOnce и до RPC; одно подтверждение нельзя применить к другой квитанции или раскрытию секрета. |
| Истечение / renewal | Нельзя продлить срок elevation обычным sliding renewal | Истёкшее подтверждение не допускает команду; renewal удаляет его, не расширяя authority. |
| Недоступность store / ошибка замены cookie | Нет подтверждения durable ConsumeOnce либо новой обычной session | Business RPC не выполняется; 503. После уже выполненного ConsumeOnce старый ID остаётся израсходованным. |
| Ошибка / timeout CP | Подтверждение уже израсходовано, исход команды может быть неизвестен | Нет автоматического повторного RPC; UI перечитывает receipt/decision и при необходимости проходит новое подтверждение с тем же idempotency key. |
| Logout | Существующая синхронная ревокация session ID | Скопированная cookie и замена pod не восстанавливают подтверждение. |

Нового event, store, фоновой задачи, ключа или deploy workload нет. Read path:
GET receipt и авторитетный revocation state. Store и key rotation/readiness
сохраняют действующий gateway lifecycle. Логи не содержат cookie/bearer,
purpose payload либо текста пользовательского решения.

# Проверки и ограничения

`TestEmailEffect*` проверяет реальные generated HTTP routes на fake gRPC client:
mapping, ETag, no-store, скрытие worker полей, входные ограничения и ошибки
authority/OCC, повреждённые ответы владельца. Общий тест security boundary
проверяет session, CSRF, revocation и несовпадение organization на обоих paths.
App test связывает оба RPC с exact authority profile без обязательного
project header.

`TestIssueEmailSession*`, `TestCreateOwnerSession*`, `TestEmailElevation*`
проверяют mapping fresh purpose, временные границы, ACR/AMR, шифрованную
cookie, key overlap и renewal. `TestEmailReconciliation*` проверяет exact
receipt binding, расходование до RPC, timeout/denial без повторного вызова,
отказ store/issuer и гонку восьми запросов через две gateway-реплики с одним
общим fake revocation store. Это локальная проверка поведения gateway, не
подтверждение CP SQL либо живого durable backend.

Документы oapi-codegen получены через Context7:
[официальный Go package](https://pkg.go.dev/github.com/oapi-codegen/oapi-codegen/v2).
Генерация сама не заменяет проверку ограничений и ответов; используется
существующий строгий JSON decoder и явные typed преобразования.

На producer checkpoint `d31cd4c70` SQL/domain receipt handlers, fresh-owner
проверка и worker trust ещё не были реализованы. HTTP тесты с fake RPC не
доказывают эти части. Их итоговая реализация и защищённый сквозной тест остаются
обязательными зависимостями #1046/#1037 до приёмки. Live mail, новые сетевые
порты и staging этим изменением не запускались.
