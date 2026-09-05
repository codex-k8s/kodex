---
id: SVC-MC-018
title: stt-tts-service
type: service
status: approved
owner: developer
version: 1.5.0
updated: 2026-09-05
---

# stt-tts-service

Активный stateless STT unit #1020 эпика #1018: входит в release image set,
`web-only` и `web-with-mattermost`. TTS не реализован. Включение сервиса в
render не является разрешением на deploy или свидетельством live acceptance.

## Сквозной контракт

| Шаг | Проверка | Результат |
| --- | --- | --- |
| Browser → gateway #1045 | session, CSRF/Origin, organization eligibility #1046, bounded upload | payload не назначает actor/organization/project |
| Gateway → STT | exact mTLS, verifier, свежий stream authority, operation/permission `platform.stt.transcribe` | domain permission `stt.transcribe`; `platform.stt.use` не RPC permission |
| STT → control-plane | continuation parent JTI, request digest, source revision/provenance, locator/echo | server-pinned policy, account/generation, limits |
| Audio → FFmpeg | MIME allowlist, actual decode, byte/sample/deadline limits | длительность по PCM, не STREAMINFO/container duration |
| STT → secret-broker | exact continuation, config/account/generation/expiry | краткоживущий key, очищаемый после запроса |
| STT → OpenAI | один HTTPS POST через exact egress-gateway, TLS 1.3, без redirect/retry | bounded transcript и безопасный receipt |
| Admin → gateway → STT catalog | organization permission `system.configuration.manage`, exact unary digest, mTLS/verifier | возможности adapter до первой configuration, без credential или provider вызова |

Project необязателен только когда отсутствует в проверенном authority.
Переданный project обязан иметь полную provenance; пустой locator не разрешает
доступ сам по себе. Изменения CP organization projection/authority принадлежат
#1046, глобальный HTTP route/bootstrap — #1045.

Результат не сохраняется в БД, cache, object storage или audit. Domain events
нет: авторитетный результат — единственный RPC response. Успех, отказ,
cancel/deadline освобождают stream slot, byte reservation и ephemeral spool.
Retry — новый явный запрос с новым authority; скрытых billable повторов нет.

## Форматы

| Входной MIME | Нормализованный MIME / контейнер |
| --- | --- |
| audio/mpeg, audio/mp3, audio/mpga | audio/mpeg / MP3; расширения mp3/mpeg/mpga |
| audio/wav, audio/x-wav, audio/wave | audio/wav / WAV |
| audio/flac, audio/x-flac | audio/flac / FLAC |
| audio/webm, video/webm | audio/webm / WebM |
| audio/ogg, application/ogg | audio/ogg / Ogg |
| audio/mp4, audio/m4a, audio/x-m4a, video/mp4 | audio/mp4 / M4A/MP4 |

Разбор через `mime.ParseMediaType`; допускается только параметр
`codecs=opus|vorbis|mp4a.40.2`. MIME не доказывает формат: FFmpeg проверяет
контейнер и декодирует samples. Демультиплексоры/кодеки закрыты allowlist,
входной protocol — только наследованный seekable `fd`, без file/HTTP/playlist.
MP4 с moov в конце доступен без дополнительной production-копии файла.
Matroska packet boundaries берутся из demuxer без повторного Opus parser;
сам decoder всегда включён. Любая диагностика decoder приводит к отказу и
не сохраняется: metadata может содержать пользовательский текст.

Hard limits: 25 MiB на файл, два stream/50 MiB зарезервированных bytes,
64 MiB memory-backed spool, 256 MiB container memory. Policy может сужать limits.
Полный stream ограничен min(20 секунд, authority expiry), decode — 5 секунд
внутри этого бюджета, provider — не более 15 секунд и всех expiry.
PCM вывод не сохраняется; превышение sample budget отменяет и дожидается
decoder. Shutdown 30 секунд, Kubernetes grace 35 секунд.

## Модель и параметры

Рекомендуемый новый профиль: `gpt-transcribe`, `languages=[ru,en]`,
пустые keywords/prompt, `temperature=0`, `response_format=json`, stream=false,
10 MiB и 120 секунд. CP #1046 хранит параметры в immutable revision и передаёт
`ResolveTranscriptionPolicyResponse.parameters`; upload их не принимает.
Каталог адаптера включает `gpt-transcribe`, `gpt-4o-transcribe`,
`gpt-4o-mini-transcribe`, документированный snapshot
`gpt-4o-mini-transcribe-2025-12-15`, а `whisper-1` помечает legacy.
Произвольные snapshots, diarization и realtime модели закрыто отклоняются.

`modelprofile.Validate` проверяет конечный temperature 0..1, language hints,
ограниченные prompt/keywords и совместимость. Только gpt-transcribe получает
`languages[]`/`keywords[]`; остальные модели получают singular `language`.
Старый policy language переводится в единственный languages[] для gpt-transcribe,
но одновременное заполнение двух hints запрещено. Chunking допускает unset/auto
для GPT, только unset для Whisper. Stream=true запрещён синхронным профилем
MVP57, хотя каталог отдельно сообщает file-stream способность провайдера.
Singular `language` для gpt-transcribe не отправляется. Ответ допускает
`text`, `usage`, `languages`; unknown/trailing JSON закрыто отклоняется.
Каталог возвращается адаптером в `GetModelCatalog` и `availability.catalog`, содержит version,
дату проверки официальной документации observed_at и server limits параметров.
Это реестр совместимости, а не live account/model availability.

### Административный каталог до первой настройки

MVP-UI-56: `GET /api/v1/system-stt/model-catalog` в #1045 использует generated
`SpeechToTextService.GetModelCatalog`. Пустой request не принимает actor,
organization, project, model, credential или configuration. CP #1046 разрешает
организацию и существующее право `organization.manage` из проверенной web session; issuer
выдаёт operation `platform.stt.model-catalog.get` с permission
`system.configuration.manage`, exact target/actor/organization и
`UNARY_PROTO_SHA256`. Project/resource/version/attempt/idempotency metadata
для этого чтения запрещены. STT проверяет конкретные method/operation/
permission/binding и provenance, запрещает project/continuation и непустой
payload. Метрики используют фиксированный bucket `model_catalog`.
RPC permission `system.configuration.manage` не является новой ACL-записью:
её выдача требует указанного авторитетного права организации в CP.

Domain читает только зарегистрированный adapter catalog: policy/credential
projection, decoder, egress/model probe и audio POST не вызываются. Ответ
содержит существующий typed каталог с version/observedAt; boolean READY,
account/credential/configuration и user availability в нём отсутствуют.
Ни выключенная конфигурация, ни её отсутствие не блокируют первоначальную
административную форму. Это не обход проверки разрешений или локального
startup barrier сервиса. Срок чтения ограничен min(5 секунд, caller deadline,
authority expiry); отменённый/истёкший запрос не возвращает каталог.

| Переход чтения | Результат и авторитетный read path |
| --- | --- |
| Успех до первой configuration/credential | Текущий каталог adapter в одном RPC response; состояние, receipt и domain event не создаются. |
| Нет/отозвано право, speech-only permission, неверные tenant provenance или binding | Отказ verifier/domain до adapter read; микрофон не становится доступным. |
| Cancel/deadline/expiry | Ограниченный отказ без внешнего effect и без сохранённого результата. |
| Повтор | Новое чтение с новым проверенным authority; прежний proof не продлевается и не заменяет replay protection. |
| Недоступный adapter catalog | Закрытая ошибка без fallback на browser enum или config READY. |

Общий `modelprofile.MaximumProviderTimeout` задаёт предел 15 секунд для
валидации owner policy и выполнения STT. Успешная публикация configuration
сама по себе не подтверждает provider readiness.

## Readiness и доступность микрофона

Provider proxy — только `egress-gateway:8081`, профиль `openai-stt`, workload
`stt-tts-service`, operation `openai.transcription`, destination
`api.openai.com:443`. Обязательные deployment-owned
`STT_EGRESS_EXPECTED_REVISION`/`STT_EGRESS_EXPECTED_DIGEST` загружаются typed
config без default/fallback. GET `/readyz` и каждый CONNECT200 требуют всех
пяти точных `X-Kodex-Egress-*` headers без дубликатов. CONNECT проверяется
через `http.Transport.OnProxyConnectResponse` до TLS и передачи credential.
Источник workload — CNI selector + listener1029, не caller header. TLS 1.3,
SNI/CA и запрет redirect остаются в STT; на8080 fallback отсутствует.
Render test вычисляет canonical digest фактической policy parser-ом1029.

`/healthz` — процесс. `/readyz` и `CheckReadiness` — local runtime,
issuer/verifier, decoder и writable spool, без удалённых вызовов.
Обычные `/diagnostics/protected-path` и `CheckProtectedPath` не имеют
user authority, поэтому не заявляют READY полного пути.

Gateway вызывает `sttapi.CheckAvailability(ctx, client)` через обычный
защищённый client-stream `Transcribe`: единственное сообщение
`availability_check: {}`, затем CloseAndRecv. Используется свежий authority
того же пользователя/organization, без project для global/admin.
Ответ — `availability {ready, stage, valid_until, catalog}`, без text/receipt.

Проверка получает реальные policy/credential projections и выполняет только
`GET https://api.openai.com/v1/models/{selected-model}` через тот же provider
HTTP client, exact TLS/egress, с общим бюджетом 5 секунд. Key очищается.
GET подтверждает доступ к model metadata, но не качество транскрипции:
для него всё равно обязателен отдельный live smoke.
Cache availability не разделяется между users/organizations и истекает
не позже min(authority, policy, credential expiry, 30 секунд).
Любой отказ или отсутствие свежего результата скрывает микрофон.

## Проверки

- `make test-stt-tts-service-contract`: оба profile render, key delivery,
  exact network, Dockerfile check и Go tests.
- `cd services/internal/stt-tts-service && GOWORK=off go test -race ./...`:
  domain/security, decoder containers, mTLS protected fake integration,
  continuation request binding, revoked authority/credential, timeout/no retry.
- `go test ./internal/providersmoke -run TestFixturePreflight -v`:
  embedded tracked fixture, 46364 bytes, SHA256
  `56a17fd3675e5913e912c404a203bc1062daf3c3c1ec79d5210d20fe28539e8e`.
- `stt-provider-smoke --fixture-only`: тот же portable preflight без ключа
  и сети, включая реальный decoder в runtime image.
- `testdata/capture-mediarecorder.cjs /absolute/disposable/directory`:
  настоящий MediaRecorder Chromium/Firefox/WebKit записывает разрешённый
  fixture через Web Audio; без hardware microphone и внешней сети.
  `STT_PLAYWRIGHT_PACKAGE` — optional package.json установленного Playwright.
  `KODEX_STT_MEDIARECORDER_FIXTURES` подключает записи к Go decoder test.
  WebKit/Linux не выдаётся за Safari/macOS или Safari/iOS.

`make test-stt-provider-smoke` без отдельного
`KODEX_STT_PROVIDER_SMOKE_OPENAI_API_KEY` — NOT RUN после успешного preflight.
По умолчанию используется embedded tracked fixture, не абсолютный home path.
Optional `KODEX_STT_ACCEPTANCE_FIXTURE` обязан иметь тот же size/SHA256.
Live Job не входит в active profiles и требует отдельного owner OK.
После выделения listener1029 identity `stt-provider-smoke` не разрешена на8081;
для live smoke нужен отдельно согласованный запуск в STT workload boundary
с тестовым ключом и deployment egress expectations. Расширять ingress или
выдавать smoke чужую workload identity нельзя. Fixture-only не требует сети.
До staging live provider не вызывать без отдельного тестового ключа.

## Проверенные документы

- [OpenAI GPT-Transcribe](https://developers.openai.com/api/docs/models/gpt-transcribe).
- [OpenAI File transcription](https://developers.openai.com/api/docs/guides/speech-to-text):
  languages вместо language и JSON languages response.
- [OpenAI Audio API](https://developers.openai.com/api/reference/resources/audio/subresources/transcriptions/methods/create).
- Повторно проверены через OpenAI Docs и Context7
  `/websites/developers_openai_api_reference`: file transcription,
  model-specific `language/languages`, keywords и response formats.
- Context7 `/websites/ffmpeg_documentation`: protocol whitelist, decode/error options.
- Context7 `/microsoft/playwright`: Chromium/Firefox/WebKit launch/close.
- Для CONNECT callback Context7 не нашёл релевантный net/http; проверены
  [официальная документация Go](https://pkg.go.dev/net/http#Transport)
  и исходник установленного Go `net/http/transport.go`.

Live account/model access и распознавание проверяются отдельно, не выводятся
из документации, Docker build или fake provider.
