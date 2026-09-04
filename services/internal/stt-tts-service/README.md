---
id: SVC-MC-018
title: stt-tts-service
type: service
status: approved
owner: developer
version: 1.1.0
updated: 2026-09-04
---

# stt-tts-service

`stt-tts-service` — подготовленный, но неактивный stateless-владелец
server-side распознавания речи. TTS отсутствует. Пока #1019, #1021, #1023 и
#1024 не материализовали producer RPC, gateway route, единый continuation
proof и authority sidecars, unit не входит в `web-only`,
`web-with-mattermost` и release image set. Base и owner overlays существуют
только для contract/render-проверки; развёртывать их сейчас нельзя.

## Сквозной контракт после materialization

| Шаг | Actor/authority и контракт | Владелец/результат |
| --- | --- | --- |
| 1 | #1021 принимает bounded browser multipart и создаёт client-stream `Transcribe` | browser payload не назначает actor/tenant/project |
| 2 | local `internalrpcauth` verifier проверяет mTLS, exact caller/target/full method/operation, permission, expiry и root actor/tenant/project provenance | transport передаёт домену только `VerifiedAuthorizationContext` |
| 3 | stream metadata резервирует до копии один из двух slots и весь заявленный byte budget; chunks не больше 64 KiB пишутся в bounded spool; commit сверяет exact size и SHA-256 | trailing message/data и mismatch закрыто отклоняются |
| 4 | policy adapter обязан получить server-owned delegated/continuation proof для root actor/tenant/project, request/correlation, source revision+digest/provenance и exact RPC | до реализации #1023 adapter возвращает ошибку до сетевого RPC; locator/echo не authority |
| 5 | домен проверяет server-pinned `gpt-transcribe`, `ru`, limits и MP3/WAV frames/chunks | FLAC отклоняется: подменяемый STREAMINFO `total_samples` не доказывает frames |
| 6 | credential adapter применяет тот же proof/locator contract и сверяет config/account/generation/expiry | #1024 выдаёт краткоживущий key; key очищается после вызова |
| 7 | один `POST https://api.openai.com/v1/audio/transcriptions` идёт через exact egress proxy без автоматического retry | multipart состоит только из streaming file, `model=gpt-transcribe`, `language=ru` |
| 8 | ответ содержит transcript и безопасный receipt | receipt не содержит transcript/audio/API key/grant |

Success receipt содержит request/correlation, actor/tenant/project, authority
source revision+digest, config revision+digest/model/language, provider account
locator/credential generation и завершённый stage. Сервис не хранит state и не
публикует domain event; повторный запрос получает новый authorization context.

## Readiness и diagnostic

- `/healthz` подтверждает жизнь процесса;
- `/readyz` и gRPC `CheckReadiness` проверяют только уже открытые local
  listeners, локальный verifier, pinned config и writable bounded spool;
- `/diagnostics/protected-path` и gRPC `CheckProtectedPath` отдельно сообщают
  stage полного protected path и не участвуют в Kubernetes readiness;
- diagnostic не вызывает OpenAI. В текущем неактивном unit он закрыто
  возвращает `delegated_authority`, потому что continuation proof #1023 ещё не
  материализован.

Readiness не зависит от control-plane, secret-broker, DNS/egress или OpenAI и
не утверждает готовность полного protected path.

## Ресурсные и lifecycle-границы

- максимум одного файла — 25 MiB, одновременно — два stream и 50 MiB
  зарезервированных audio bytes;
- spool `emptyDir` — 64 MiB, Pod memory limit — 256 MiB;
- request deadline — 20 s, provider timeout — не более 15 s;
- shutdown budget — 30 s, Kubernetes grace — 35 s;
- gRPC сначала выполняет deadline-aware `GracefulStop`, после deadline —
  `Stop` и bounded join;
- один provider effect, автоматического retry нет.

## Наблюдаемость

`kodex_stt_tts_service_transcription_stage_total{stage,error_class}` использует
только закрытые stage `authority|policy|audio|credential|egress|provider|success`
и error class `none|denied|invalid|unavailable|timeout|rejected`; неизвестные
значения нормализуются в `unknown`. Логи и receipt не содержат audio,
transcript, API key или authority grant.

## Acceptance fixture

`make test-stt-acceptance` использует внешний
`KODEX_STT_ACCEPTANCE_FIXTURE` (по умолчанию
`/home/s/projects/matter-codex/.agents/mvp-finish/1-2-3-4-5.mp3`) и до live
вызова требует exact SHA-256
`56a17fd3675e5913e912c404a203bc1062daf3c3c1ec79d5210d20fe28539e8e`.
Credential для локального теста задаётся только
`KODEX_STT_ACCEPTANCE_OPENAI_API_KEY`; без него результат — честный `NOT RUN`.

Неактивный code-first launcher расположен в
`deploy/k8s/base/stt-tts-service-acceptance`: Job использует тот же
`egress-gateway`, внешний PVC fixture и Secret file без значения в Git,
`backoffLimit: 0`. Его запуск требует отдельного owner OK и materialized
fixture/Secret; в этом remediation deploy не выполняется.

## Проверенные внешние документы

Проверены официальный OpenAI Audio Transcriptions create reference (endpoint,
поддерживаемые форматы и параметры `file`, `model`, `language`), Context7
`/grpc/grpc-go` для `GracefulStop`/`Stop` и Context7 Go standard library
`/websites/pkg_go_dev_go1_25_3` для streaming `mime/multipart`/`net/http` body.
