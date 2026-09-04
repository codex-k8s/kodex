---
id: STT-CHECK-1020
title: Локальная проверка активации STT
type: verification
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-05
---

# Проверка #1020

База: main `1cf399a5f`, включая scheduler1027/policy44. Общую authority policy
этот unit не изменяет. CP1046/policy45 и HTTP1045 интегрирует root; глобальная
browser-проверка их объединённого результата не является локальным STT тестом.

## Критерий → свидетельство

Дополнение1029: checkpoint `fd93e6f4ebd254be41fcb4cc9e7a4775a20f932b`
слит с сохранением consumer NetworkPolicy8081. STT expectations сверяются
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

- Live OpenAI: отдельный тестовый ключ не предоставлен. Direct smoke после
  успешного fixture preflight выдаёт NOT RUN, не PASS.
- Safari macOS/iOS и hardware microphone: среда отсутствует. Linux WebKit
  26.5 не предоставляет MediaRecorder; его case SKIP/NOT RUN, не Safari PASS.
- Staging/deploy, registry promotion/node pull, живые issuer/readback и
  финальное объединённое browser acceptance: разрешение не выдавалось.

Периодический refresh Bootstrap по TTL вызывает свежий probe через
пользовательский authenticated stream. Отдельный незащищённый account-wide
обход не используется; key не сохраняется между probes. Model metadata GET
не доказывает платную транскрипцию и не заменяет live smoke.

Push, PR, merge и deploy этим unit не выполняются. Итоговый SHA и локальные
результаты сообщаются root отдельно; per-unit review отменён владельцем.
