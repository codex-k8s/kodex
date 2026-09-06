---
id: RUN-MC-027
title: Диагностика stt-tts-service
type: runbook
status: approved
owner: sre
version: 1.5.0
updated: 2026-09-05
---

# Диагностика stt-tts-service

STT входит в release image set и оба профиля. Применение render и live smoke
требуют отдельного owner OK. Audio, transcript, prompt, metadata, API key и
authority proof нельзя публиковать в evidence или логах.

## Readiness

При `StartError` до запуска процесса сверить `STT_SPOOL_DIRECTORY` и mountPath:
`/var/lib/kodex/stt-spool`. Writable spool не должен попадать под read-only
`/run/kodex`, включая alias `/var/run -> /run`; не снимать read-only authority
или non-root. Оба профиля проверяет `make test-stt-tts-service-contract`.

`/healthz` подтверждает процесс; `/readyz` и `CheckReadiness` —
локальные issuer/verifier/config/decoder/spool. Они не подтверждают provider.
Неаутентифицированная техническая диагностика полного пути всегда сообщает
`ready=false, stage=delegated_authority`, даже если local issuer готов.

Доступность для конкретного пользователя проверяется
`sttapi.CheckAvailability`: тот же защищённый `Transcribe` stream,
единственный `availability_check`, затем CloseAndRecv.
Проверяются actual continuation policy/credential и model GET через exact
HTTPS egress. Ответ не содержит ключа, текста или receipt; свежесть ограничена
`valid_until`. Model metadata GET не заменяет live transcription acceptance.

## Проверка инцидента

Для первоначальной административной настройки отдельный `GetModelCatalog`
возвращает только совместимость adapter по signed RPC permission
`system.configuration.manage`, полученной после проверки `organization.manage`
в CP. CP не требует существующей STT configuration
или credential. Этот unary RPC не обращается к OpenAI и не сообщает READY.
Отказ здесь проверяется по admin authority/issuer/verifier и adapter catalog,
а отказ микрофона — по пользовательскому protected path выше. Domain,
projection и owner policy используют общий максимум provider timeout 15 секунд.

1. Отделить local readiness от пользовательской availability; не показывать
   микрофон по одному readyz.
2. Для authority/policy/credential сверить exact actor/organization,
   optional project, source/config revision+digest и account generation.
   RPC permission `platform.stt.transcribe` отображается в `stt.transcribe`;
   `platform.stt.use` не заменяет permission.
3. Для audio проверить size/commit SHA и поддержанный MIME без публикации
   файла. FFmpeg декодирует MP3/WAV/FLAC/WebM/Ogg/MP4/M4A; duration только
   из decoded samples, без доверия header. Decoder errors не логируются.
4. Для egress проверить exact proxy, DNS, CONNECT/SNI `api.openai.com:443`,
   TLS CA и встречные ingress policies. Direct HTTPS и skipTLSVerify запрещены.
5. Для provider учитывать возможный billable effect; скрытый retry запрещён.
   Повтор — новое явное действие пользователя.
6. Проверить issuer/verifier key-delivery targets, exact publisher Role,
   served-state readback и restore path. Ручной Secret не заменяет publisher.

Метрики stage/error_class имеют закрытые множества. Stream telemetry включает
отказы до первого сообщения. Correlation допустим только как безопасная
диагностическая привязка, не metric label.

## Smoke и rollback

Tracked fixture содержит 46364 байта, SHA256
`56a17fd3675e5913e912c404a203bc1062daf3c3c1ec79d5210d20fe28539e8e`.
Default smoke использует embedded копию; external override проверяет тот же hash.
Без отдельного тестового credential live smoke — NOT RUN.
Job `stt-provider-smoke` не входит в profiles, имеет backoffLimit=0;
его запуск отдельно разрешает владелец. Direct smoke не проверяет
gateway/authority; для локальной интеграции есть TestProtectedFakeIntegration.

При регрессии отключить STT через авторитетную configuration команду владельца,
затем применить согласованный release. Secret/key/policy revisions не откатывать:
ротация и recovery только forward-only по общему authority runbook.
Изменения HTTP принадлежат #1045, organization eligibility/projection — #1046.
