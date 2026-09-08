---
id: OPS-DOC-1243
title: Голосовой ввод и локальные доказательства STT lifecycle
type: verification
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-08
---

# Голосовой ввод #1243

Источник: [Issue #1243](https://github.com/codex-k8s/kodex/issues/1243),
[каноническая приёмка #1031](mvp-1031-acceptance.md), требования
MVP-UI-14/55–60. База изменений:
`6061aedc558330d65215e7512df6fd457cfb9e78`. Проверки выполнены локально
на дереве изменений этого PR; точный commit закреплён в описании PR.
Владелец разрешил тематическую поставку и локальные проверки без review.

Исправлены реальные пробелы общего UI: системный stop больше не разрешает
отправить запись; лимит длительности завершает capture безопасной ошибкой;
старый callback track не прерывает новую запись; pagehide отменяет request;
CodeMirror изолирует voice transaction с обеих сторон истории. Пользователь
получает отдельные безопасные причины отказа микрофона, codec, лимита,
пустой записи и typed backend failures. Runtime-диагностика не содержит
исходный browser error. Горизонтальный scroll сохраняется наряду с вертикальным.

Существующие backend/configuration/permission пути сохранены. Старый
protected MP3 smoke `mvpr1231-stt1` на STT `5fd17445` с tool source `3edbf2e`
передан корневым manager как исторический PASS. Здесь он не воспроизводился
и не является доказательством нового SHA, UI lifecycle или всей приёмки.
TTS не входит в поставку. Credentials и пользовательское аудио не читались;
в browser использованы только синтетический media device и fixture transcript.

## Сквозной сценарий и authority

| Этап      | Исполняемый путь и владелец                                                                                                                                                                              |
| --------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Допуск UI | `features/speech/useSpeechInput.ts` и `SpeechAvailabilityLease`: authenticated session, authoritative bootstrap availability с `validUntil`, bounded refresh и закрытие по expiry/отзыву                 |
| Запись    | `shared/ui/VoiceInputButton.vue` → `VoiceCapture`: один активный capture, browser microphone permission, ограниченные chunks и timer; локальный флаг не выдаёт permission                                |
| Запрос    | `shared/api/speech.ts` → generated `transcribeOrganizationSpeech`, `POST /api/v1/speech/transcriptions`, session/CSRF/Origin и `X-Audio-Size`; browser не выбирает key/model/actor                       |
| Gateway   | `internal/transport/http/stt_endpoint.go`: bounded multipart/chunks/size/digest commit → protected streaming `stt.v1.SpeechToTextService/Transcribe`                                                     |
| Authority | Control-plane разрешает точные user/organization и `speech.transcribe`; STT проверяет signed principal, revision/digest, deadline и policy/credential projections с exact account/generation/config pins |
| Provider  | `transcription.Service.Transcribe` → model-specific validation → decoder → credential projection → OpenAI adapter через exact egress/TLS; один POST, без автоматического повтора                         |
| Результат | Нормализованный transcript и безопасный receipt возвращаются по тому же HTTP request; textarea/native undo либо CodeMirror transaction применяют результат только к живому capture                       |

Idempotency/STT retry не создают повторный provider effect автоматически.
Отдельной durable STT task, очереди retry или domain event для browser capture
нет: это ограниченный синхронный запрос. Authoritative read path допуска и
конфигурации принадлежит bootstrap/control-plane; UI не восстанавливает grant
из старого ответа. Изменение конфигурации остаётся штатным managed revision
lifecycle с owner OCC/idempotency, публикацией, привязкой и readback.

## Матрица capture lifecycle

| Переход                                    | Результат                                                                                         | Локальное доказательство                                                                                       |
| ------------------------------------------ | ------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- |
| idle → requesting → recording              | Codec до microphone prompt; только актуальный stream принимается                                  | `voice-input.test.ts`: availability, unsupported codec, отсутствующий MediaRecorder                            |
| Browser deny / ошибка открытия             | Без provider call; закрытая причина, исходная диагностика не сохраняется                          | Unit `NotAllowedError`/`SecurityError`; browser unsupported codec                                              |
| Пользователь повторно нажал stop           | Последний dataavailable → один transcribe; tracks освобождены до ожидания                         | Unit основного пути и повторного stop; Chromium MediaRecorder                                                  |
| Device/browser stop, track ended           | error, tracks/chunks/timer освобождены, аудио не отправлено                                       | Unit неожиданного stop до track ended и старого callback                                                       |
| Размер/длительность превышены              | error без отправки; следующая запись только явным действием                                       | Unit 10 MiB и 120 секунд                                                                                       |
| Cancel при microphone prompt               | Поздний getUserMedia stream остановлен, не принимается                                            | Unit позднего stream                                                                                           |
| Cancel после отправки                      | Abort signal; поздний transcript/error не применяется                                             | Unit обоих late outcomes; browser cancel                                                                       |
| Navigation / unmount / pagehide            | Старый capture/request закрыт; поле не изменяется поздним ответом                                 | Четыре browser regression сценария, включая утрату availability                                                |
| Logout / permission/config expiry          | Session watcher закрывает lease; synchronous visibility watcher отменяет capture                  | Lease unit: stop/expiry/false/stale bootstrap; browser утрата availability. Полный login/logout с BFF: NOT RUN |
| Второе поле начинает запись                | Прежний capture отменён; одновременно активен один recorder                                       | Browser единственной активной записи                                                                           |
| Provider timeout/rate limit                | Typed error; нет автоматического повторного POST                                                  | OpenAI/gRPC/gateway unit; browser HTTP429 fixture и ожидание без retry                                         |
| Success в textarea                         | Текущее selection заменено, focus/scroll сохранены, native undo возвращает прежний текст          | Chromium 390/1440 px и длинный textarea                                                                        |
| Success в CodeMirror                       | `replaceSelection`, `isolateHistory("full")`, focus/scroll; отдельный undo до/после ручного ввода | Unit истории/обоих scroll axes и Chromium 390/1440 px                                                          |
| Sensitive / readonly / disabled / fieldset | Кнопки нет; отключение во время записи отменяет capture                                           | Browser обычных полей, code editors и SYSTEM_STT managed fields                                                |

Cancel после отправки не доказывает отсутствие уже выполненного provider
effect. Восстановление UI никогда не повторяет прежний audio upload.

## Матрица требований и остаток live acceptance

| Требование | Path и локальное доказательство                                                                                                                         | Оставшаяся живая проверка                                                                                                                                                         |
| ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| MVP-UI-14  | Общие VoiceTextarea/VoiceInputButton/code-editor-keymap; browser selection, undo, sensitive/readonly/disabled и fieldset PASS                           | Полный каталог пользовательских страниц, пересечения resize/send/validation на реальном layout                                                                                    |
| MVP-UI-55  | Существующий отдельный `stt-tts-service`, OpenAI adapter и domain/transport/egress tests, Go binary build PASS; TTS API не добавлялся                   | Фактический release render, registry/node pull, deploy ownership и egress в среде                                                                                                 |
| MVP-UI-56  | STT activation/model-catalog PWA units; control-plane revision/STT eligibility units; `sttapi/modelprofile` и adapter multipart parameter tests PASS    | Реальные configuration create/publish/bind/rebind, OCC/replay, reload, permission variants и credential revoke; PostgreSQL component suite здесь NOT RUN                          |
| MVP-UI-57  | Gateway multipart/format/size/cancel/receipt/error tests, STT signed principal/projection/negative tests и protected fake integration PASS              | Развёрнутые session/CSRF/Origin, выбранный/отсутствующий project, revoked key/model, реальные quota/timeout и exact service chain                                                 |
| MVP-UI-58  | 15 synthetic Chromium browser scenarios плюс VoiceCapture/lease units PASS; физическое устройство не используется                                       | Аппаратный microphone, реальный browser permission revoke, полная logout/auth интеграция, Firefox/Safari/iOS, Permissions-Policy итогового ingress                                |
| MVP-UI-59  | Local readiness, bounded admission/deadlines, spool cleanup, decoder cancellation, safe diagnostics/metrics и exact egress model probe unit suites PASS | Фактические сертификаты/Secret projection, NetworkPolicy, organization/subject quota нескольких replicas, shutdown, метрики/alerts и отсутствие чувствительных данных в telemetry |
| MVP-UI-60  | Tracked MP3 checksum/normalizer preflight в Go tests PASS; fake provider и browser synthetic capture PASS                                               | Новый real OpenAI/HTTP/UI smoke NOT RUN: платные вызовы запрещены. Старый PASS не переносится на этот SHA                                                                         |

## Выполненные проверки

Все результаты ниже локальные, не GitHub CI. Backend исходники не менялись.

| Команда                                                                                                                                                                                                                                                                                                  | Результат                                                                                  |
| -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------ |
| В PWA: `npm run test:unit -- src/shared/ui/voice-input.test.ts src/shared/ui/code-editor-keymap.test.ts src/features/speech/availability.test.ts src/shared/api/speech.test.ts src/features/managed-configurations/stt-activation.test.ts src/features/managed-configurations/stt-model-catalog.test.ts` | PASS, 6 файлов / 38 tests                                                                  |
| В PWA: `npm run build`                                                                                                                                                                                                                                                                                   | PASS, включая `vue-tsc --build --force`; существующее предупреждение Vite о chunks >500 kB |
| В PWA: `npm run build:synthetic`                                                                                                                                                                                                                                                                         | PASS                                                                                       |
| В PWA: `node_modules/.bin/playwright test --config playwright.synthetic.config.ts e2e/voice.synthetic.spec.ts`                                                                                                                                                                                           | PASS, исходные 14 scenarios / 25.2 s                                                       |
| В PWA: та же команда с `--grep 'сохраняет scroll'`                                                                                                                                                                                                                                                       | PASS, дополнительный сценарий / 1.6 s; всего 15                                            |
| Scoped ESLint изменённых TS/Vue/e2e и Prettier                                                                                                                                                                                                                                                           | PASS после удаления лишнего type assertion, обнаруженного первым ESLint запуском           |
| В STT: `GOMAXPROCS=4 GOWORK=off go test -p 2 -timeout 120s ./...`                                                                                                                                                                                                                                        | PASS; optional real-provider и externally configured browser-container tests SKIP/NOT RUN  |
| В STT: `GOMAXPROCS=4 GOWORK=off go build -p 2 -o /dev/null ./cmd/stt-tts-service`                                                                                                                                                                                                                        | PASS                                                                                       |
| В API gateway: `GOMAXPROCS=4 GOWORK=off go test -p 2 -timeout 120s ./internal/transport/http -run 'Speech\|STT\|Stt\|ForwardAudio'`                                                                                                                                                                      | PASS                                                                                       |
| В API gateway: `GOMAXPROCS=4 GOWORK=off go test -p 2 -timeout 90s ./internal/sttclient`                                                                                                                                                                                                                  | PASS; отдельный непустой запуск после слишком узкого общего фильтра                        |
| В API gateway: `GOMAXPROCS=4 GOWORK=off go build -p 2 -o /dev/null ./cmd/control-api-gateway`                                                                                                                                                                                                            | PASS                                                                                       |
| В `libs/go/sttapi`: `GOMAXPROCS=4 GOWORK=off go test -p 2 -timeout 90s ./...`                                                                                                                                                                                                                            | PASS                                                                                       |
| В control-plane: `GOMAXPROCS=4 GOWORK=off go test -p 2 -timeout 90s ./internal/domain/service/revision ./internal/repository/postgres/platform -run 'STT\|Stt\|Speech'`                                                                                                                                  | PASS для герметичных units; PostgreSQL component tests не запускались                      |

Полный baseline, Kubernetes render/Docker contract suite, staging/production,
новый real-provider smoke и hardware browser acceptance: **NOT RUN**.
Ни один пропущенный optional test не считается PASS.

## Ручная проверка после отдельного разрешённого deploy

1. На точном обслуживаемом SHA войти пользователем с `speech.transcribe`;
   подтвердить свежую enabled SYSTEM_STT revision и readiness без нового
   credential setup. Не менять существующие fixture bindings вслепую.
2. Пройти textarea и Markdown/YAML/TOML/JSON/Dockerfile: диктовка в курсор и
   selection, focus/scroll, один undo, adjacent typing, отсутствие перекрытий.
3. Пройти cancel, navigation, logout, unmount, browser deny/device revoke,
   unsupported codec, oversize/duration limit; системное завершение не должно
   увеличивать число audio POST. В sensitive/readonly/disabled действий нет.
4. Проверить отсутствие права, выключенную конфигурацию, revoke credential,
   несовместимую модель, timeout/429 и восстановление после fresh availability.
   Не использовать обход permission и не повторять неизвестный provider effect.
5. Только после отдельного разрешения платного smoke использовать каноническую
   команду `node tools/dev/stt-http-acceptance.mjs --expected-sha "$EXPECTED_SHA"`
   и guard/journal правила #1031. Записывать match/digest, не audio/transcript.

Риск: браузеры различаются native textarea undo и MediaRecorder; Chromium
доказан локально, остальные остаются отдельной приёмкой. Безопасное превышение
лимита теперь требует новой более короткой записи. Rollback: вернуть только
этот PWA commit через отдельный PR и прежний проверенный image; backend schema
и configuration migration отсутствуют. Откат возвращает прежние voice риски,
поэтому предпочтительно исправление вперёд.

Через Context7 проверены `/mdn/content` (MediaRecorder stop/track ended),
`/websites/vuejs` (watch cleanup) и `/websites/codemirror_net` (transactions,
selection/history). Дополнительно проверен официальный
[OpenAI file transcription guide](https://developers.openai.com/api/docs/guides/speech-to-text).
Новый внешний контракт не вводился; ключи, пользовательские аудио/тексты и
provider payload не публиковались.
