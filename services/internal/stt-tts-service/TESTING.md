---
id: STT-CHECK-1020
title: Локальная проверка активации STT
type: verification
status: approved
owner: developer
version: 1.1.1
updated: 2026-09-06
---

# Проверка #1020

## Provider rate limit (#1088)

Upstream HTTP429 теперь проходит `ErrProviderRateLimited` → gRPC
ResourceExhausted с ErrorInfo `kodex.stt/TRANSCRIPTION_RATE_LIMITED`.
Общий `sttapi/errorprofile` задаёт максимум RetryInfo 300 секунд. Принимается
единственный Retry-After: целые секунды 1–300 либо HTTP-date с округлением
ожидания вверх. Missing/malformed/duplicate/слишком большой hint не становится
искусственным коротким ожиданием: RetryInfo отсутствует. Provider response
body не читается для ошибки и не включается в transport diagnostics.

Матрица: verified browser actor → HTTP streaming → existing protected STT
grant → один OpenAI POST → typed error → HTTP429/SDK/UI. Новой task, grant,
idempotency receipt или event нет: прежний synchronous request завершается
ошибкой, автоматический повтор billable effect запрещён. Следующий запрос
может быть только новым явным действием с новой authority. HTTP mapper и PWA
consumer поставляются в своих существующих unit; их checks здесь NOT RUN.

Локально новый STT full race/vet/build PASS; OpenAI adapter1.032 с и protected
gRPC2.439 с. Protected fixture проверяет upstream429 с valid/missing/malformed/
huge hint и ровно один POST на каждый вызов. Отдельные тесты покрывают HTTP-date
ceil, duplicate, отрицательные/дробные/overflow значения, private body и
непригодный доменный duration. Live OpenAI credential отсутствует, live smoke
NOT RUN; no production/provider effect. Общий review/acceptance #1031 NOT RUN.

Проверены Context7 `/websites/developers_openai_api` и
[официальные OpenAI error codes](https://developers.openai.com/api/docs/guides/error-codes).
Документация требует не сокращать Retry-After и различает rate/quota причины
HTTP429; сервис не обещает временный характер отказа без пригодного hint.
Предел 300 секунд — локальный контракт безопасного hint, не лимит OpenAI и не
расширение 15-секундного execution budget. Модель и параметры не изменены.

База: main `dfa54dab9`, включая scheduler1027 и STT egress #1052. Общую authority policy
этот unit не изменяет. CP1046 и HTTP1045 интегрирует root; глобальная
browser-проверка их объединённого результата не является локальным STT тестом.

## Критерий → свидетельство

Регрессия #1074: `TestNormalizeRussianAllowedDifferences` и
`TestNormalizeRussianPreservesSignificantDifferences` ограничивают сравнение
MVP-UI-60 регистром, Unicode whitespace и конечной пунктуацией. Внутренние
символы, начальная пунктуация, ё/е, потеря, перестановка и склейка слов не
нормализуются в совпадение. Тесты используют искусственные строки, а сообщения
об ошибках не печатают их содержимое. Проверена документация Context7
`/golang/go`: `strings.Fields`, `ToLower`, `TrimRightFunc`.

Продолжение MVP-UI-56 от main `8026633a9`: `GetModelCatalog` имеет отдельный
admin authority и доступен до первой configuration/credential. Новые
`TestCatalogPrincipalUsesIndependentOrganizationPermission`,
`TestCatalogPrincipalRejectsAuthoritySubstitution`,
`TestGetModelCatalogBeforeConfigurationHasNoRemoteEffect`,
`TestGetModelCatalogRejectsAuthorityExpiryAndCancellation` и сценарий
`TestProtectedFakeIntegration/catalog_before_configuration` проверяют
domain и настоящий mTLS/generated unary path с fake verifier. Проверяются
speech-only/отозванное право, неверные binding/provenance, неизвестный payload,
cancel и отсутствие policy/credential/provider effects. Общий максимум
provider timeout покрыт `TestPolicyProviderTimeoutUsesAdapterLimit`.
Live CP issuer/verifier, browser и OpenAI по-прежнему требуют общей приёмки.

Дополнение1029: checkpoint `fd93e6f4ebd254be41fcb4cc9e7a4775a20f932b`
интегрирован с сохранением consumer NetworkPolicy8081. STT expectations сверяются
с canonical digest и profile фактической rendered policy.
`TestEgressReadbackRequiresEveryExactHeader` покрывает absent/wrong/duplicate
для revision/digest/profile/workload/operation и 204/503.
`TestEveryCONNECTChecksGenerationBeforeTLS` использует реальные локальные
TCP/HTTP CONNECT: второй proxy response с устаревшим поколением не получает
даже TLS ClientHello. Ключи и audio в этом тесте не передаются.

| Критерий | Локальная проверка |
| --- | --- |
| Все девять расширений, реальная длительность | `TestAudioContainersDecodedSamplesAndBounds`: FFmpeg 8.0.1, реальные контейнеры; size/sample limits и обрезанный контейнер для каждого |
| Не доверять header duration | `TestAudioCancellationAndFalseFLACDuration`: STREAMINFO без frames отклонён |
| Bounded decoder | `TestRunningDecoderIsKilledAndJoinedOnDeadline`: запущенный процесс остановлен, join и cleanup выполнены |
| Browser container | Chromium 149.0.7827.55 WebM/Opus и Firefox 151.0 Ogg/Opus: MediaRecorder capture плюс `TestRealMediaRecorderContainers` |
| Portable fixture | `TestFixturePreflight`: embedded 46364 bytes, SHA256 `56a17fd3675e5913e912c404a203bc1062daf3c3c1ec79d5210d20fe28539e8e` |
| Organization без project | `TestProtectedFakeIntegration`: mTLS gateway/STT/producer, verifier context, exact continuation digest, policy/credential echo, provider effect |
| Отрицательная authority | domain/projection/transport suites: отсутствующая/отозванная authority, неверная permission/provenance, revoked credential, пропавшая policy |
| Нет ложной готовности | local readiness отдельно; authenticated availability проходит projections и model GET, без audio POST; valid_until ограничен expiry |
| Каталог и параметры | `modelprofile.TestClosedModelCompatibility`, `TestCatalogModelMultipartParameters`; сквозной fake projection переносит languages/keywords/prompt/temperature/chunking |
| Нет blind retry | fake provider 429/timeout: один POST; malformed audio не достигает provider |
| Release/deploy/key delivery | `make test-stt-tts-service-contract test-web-only-release test-install-contract`: оба профиля, image registry, Certificate, точные Secret RBAC, startup readback, network union, 13 install projections |
| Контракт/Go | STT-targeted buf lint/generate, sttapi и service `go test -race ./...`, service `go vet ./...` |
| Runtime image | Docker buildx build/check; `stt-provider-smoke --fixture-only` внутри nonroot/read-only image с `--network none` и bounded tmpfs |

Тестовая authority и HTTP provider синтетические; mTLS transport, stream
admission, generated clients, domain, projection binding и decoder реальные.
Это не запуск живых control-plane/secret-broker/authority sidecars или OpenAI.

## Воспроизведение

Из корня: `make test-stt-tts-service-contract test-web-only-release test-install-contract`.
В `services/internal/stt-tts-service`: удалить из окружения
`KODEX_STT_PROVIDER_SMOKE_OPENAI_API_KEY`, выполнить
`GOWORK=off go test -race ./...` и `GOWORK=off go vet ./...`.

Для browser capture нужен установленный Playwright и его browsers:
`node services/internal/stt-tts-service/testdata/capture-mediarecorder.cjs /tmp/stt-mediarecorder`.
Optional `STT_PLAYWRIGHT_PACKAGE` указывает package.json установки Playwright.
Повторить Go decoder tests с `KODEX_STT_MEDIARECORDER_FIXTURES=/tmp/stt-mediarecorder`.
Исходная fixture разрешена владельцем; временные результаты не добавляются в Git.

## NOT RUN

- Live OpenAI: в локальных проверках тестовый ключ не используется. Direct smoke после
  успешного fixture preflight выдаёт NOT RUN, не PASS.
- Safari macOS/iOS и hardware microphone: среда отсутствует. Linux WebKit
  26.5 не предоставляет MediaRecorder; его case SKIP/NOT RUN, не Safari PASS.
- Staging/deploy, registry promotion/node pull, живые issuer/readback и
  финальное объединённое browser acceptance: выполняются после полной
  интеграции эпика по разрешённому плану #1031, здесь не запускались.

Периодический refresh Bootstrap по TTL вызывает свежий probe через
пользовательский authenticated stream. Отдельный незащищённый account-wide
обход не используется; key не сохраняется между probes. Model metadata GET
не доказывает платную транскрипцию и не заменяет live smoke.

Root публикует unit после проверок точного SHA. Per-unit review отменён
владельцем только в ускоренной волне; общий итоговый gate сохраняется.
Tracing/Sentry cleanup зарегистрирован сразу после создания telemetry и
выполняется также при раннем startup failure, с независимыми ограниченными
контекстами и без повторного flush при штатном shutdown.
