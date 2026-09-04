---
id: SVC-MC-018
title: stt-tts-service
type: service
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-04
---

# stt-tts-service

`stt-tts-service` — stateless-владелец server-side распознавания речи. В этой
версии публичен только `stt.v1.SpeechToTextService`; TTS отсутствует в Proto,
коде и deploy-профиле.

## Сквозной сценарий

| Шаг | Контракт и источник полномочий | Владелец/результат |
| --- | --- | --- |
| 1 | `POST /api/v1/projects/{projectRef}/transcriptions` из #1021 принимает bounded multipart; browser session и CSRF проверяет `control-api-gateway` | gateway не назначает actor/tenant из multipart |
| 2 | gateway вызывает `/stt.v1.SpeechToTextService/Transcribe`; exact operation `platform.stt.transcribe`, permission `stt.transcribe`, audience и caller workload проверяет authority profile #1023 | `stt-tts-service` получает actor/tenant/project только из `VerifiedAuthorizationContext` |
| 3 | сервис вызывает `/stt.v1.TranscriptionPolicyProjectionService/ResolveTranscriptionPolicy` с проверенными locators и authority revision/digest | `control-plane` #1019 повторно проверяет tenant/project eligibility и возвращает immutable config revision, `gpt-transcribe`, `ru`, limits, provider account generation и краткоживущий grant |
| 4 | локальная проверка связывает MIME, magic bytes, размер и длительность; MP3/WAV/FLAC вне limits отклоняются до внешнего effect | состояние не записывается; OCC и idempotency неприменимы к stateless read/effect |
| 5 | `/stt.v1.TranscriptionCredentialProjectionService/ProjectTranscriptionCredential` передаёт exact policy/grant/actor/tenant/config/generation locators | `secret-broker` #1024 проверяет grant и возвращает краткоживущий API key с exact readback; ключ очищается после вызова |
| 6 | `POST https://api.openai.com/v1/audio/transcriptions` через exact proxy `egress-gateway...:8080`; TLS завершается только в OpenAI | ответ JSON `text` нормализуется только по краям и возвращается gateway; domain event отсутствует |

Поле request, `projectRef`, actor/tenant locator и provider account reference не
являются authority. Каждый producer обязан сверить их с собственным
проверенным authorization context и server-owned state. Несовпадение любого
echo/revision/digest/generation или expiry закрывает путь до OpenAI.

## Конфигурация provider

Закрытая shipped schema текущей версии:

- `model` — только `gpt-transcribe`;
- `language` — только ISO-639-1 hint `ru`;
- `maximum_audio_bytes` — `1024..26214400`;
- `maximum_audio_duration_milliseconds` — `1000..1800000`;
- `provider_timeout_milliseconds` — `1000..45000`;
- `provider_account_ref`, credential generation, revision, digest, grant и
  expiry назначаются сервером.

Произвольные `prompt`, temperature, response format и model из browser/RPC не
принимаются. MVP принимает MP3, WAV и FLAC: для них длительность вычисляется
локально без decoder/provider-вызова. Расширение списка требует нового
валидатора длительности и contract-теста. Один replica одновременно исполняет
не более двух transcription-запросов; переполнение закрыто возвращает
`ResourceExhausted/RATE_LIMITED` до provider effect.

## State, events и lifecycle

Сервис не владеет PostgreSQL/Redis/S3 state. Поэтому migrations, transaction,
OCC, idempotency receipt, outbox, AsyncAPI и domain event отсутствуют осознанно.
Один запрос имеет только один provider effect; автоматический retry после
начала OpenAI request запрещён. Retry создаёт новый пользовательский запрос и
новый authorization context/credential projection.

Startup barrier и `/readyz` проверяют verifier, issuer, принадлежащий
`control-plane` proof resolver, policy
projection, credential projection и локальную provider-конфигурацию. Worker
readiness прекращается до shutdown зависимостей; tracing и Sentry получают
раздельные bounded shutdown contexts.

## Consumer prerequisites

- #1019 материализует producer RPC policy projection и immutable STT config;
- #1024 материализует producer RPC credential projection без долговременной
  выдачи ключа;
- #1023 регистрирует workload `stt-tts-service`, SPIFFE/mTLS, issuer/verifier,
  grants и exact operation profiles всех четырёх projection RPC и входных
  `Transcribe`/`CheckReadiness`;
- #1021 добавляет внешний bounded multipart endpoint и generated STT client.

Base manifests намеренно содержат read-only slots `authority-sockets` и
`application-grant`. До атомарной materialization из #1023 Pod остаётся
неготовым; пустой volume не считается credential или готовым authority path.

## Локальная проверка

```bash
cd services/internal/stt-tts-service
GOWORK=off go test ./...
```

Live acceptance запускается только при отдельном тестовом
`KODEX_STT_ACCEPTANCE_OPENAI_API_KEY`; fixture задаётся
`KODEX_STT_ACCEPTANCE_FIXTURE`, по умолчанию используется внешний
`/home/s/projects/matter-codex/.agents/mvp-finish/1-2-3-4-5.mp3`.

При реализации сверены Context7
`/websites/developers_openai_api_reference`, официальный метод
[`audio/transcriptions create`](https://developers.openai.com/api/reference/resources/audio/subresources/transcriptions/methods/create),
карточки [`gpt-transcribe`](https://developers.openai.com/api/docs/models/gpt-transcribe)
и [`gpt-4o-transcribe`](https://developers.openai.com/api/docs/models/gpt-4o-transcribe),
а также актуальный gRPC-Go `/grpc/grpc-go` для TLS client/server и unary
interceptors.
