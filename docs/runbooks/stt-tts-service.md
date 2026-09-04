---
id: RUN-MC-027
title: Диагностика stt-tts-service
type: runbook
status: approved
owner: sre
version: 1.3.0
updated: 2026-09-04
---

# Диагностика stt-tts-service

`stt-tts-service` не входит в shipped profiles до завершения #1021. Policy
foundation #1019/#1032, continuation proof #1023 и credential projection #1024
через PR #1034 уже материализованы. Этот runbook описывает неактивный base
deployable и не разрешает его deployment. Не копируйте в evidence audio,
transcript, API key, authority proof/grant или provider body.

## Сигналы

- `/healthz` — процесс отвечает;
- `/readyz` — только local runtime: verifier/config/writable spool;
- `/diagnostics/protected-path` — отдельный JSON readback с bounded полями
  `ready` и `stage`; не является Kubernetes readiness и не вызывает provider;
- `kodex_stt_tts_service_readiness` — local readiness;
- `kodex_stt_tts_service_grpc_requests_total{operation,code}` и
  `kodex_stt_tts_service_grpc_request_duration_seconds{operation}` — все
  завершённые unary/stream RPC, включая ранние отказы;
- `kodex_stt_tts_service_grpc_streams_in_flight{operation}` — текущие stream от
  начала общей технической цепочки, включая раннюю проверку authority и
  admission;
- `kodex_stt_tts_service_transcription_stage_total{stage,error_class}` — один
  итог каждого transcription path. `stage` ограничен
  `authority|policy|audio|credential|egress|provider|success|unknown`,
  `error_class` —
  `none|denied|invalid|unavailable|timeout|rejected|unknown`.

Лог `unexpected gRPC failure` содержит только bounded `method`, canonical
`code` и server-owned `correlation_id`; edge local readiness — только
`error_class=local_runtime`. Correlation не является metric label. Payload,
audio, transcript, credential и grant не логируются.

## Разделение readiness и protected path

Успешный `/readyz` означает лишь способность Pod безопасно принять RPC. Он не
проверяет и не обещает доступность control-plane, secret-broker, egress-gateway
или OpenAI. `/diagnostics/protected-path` проверяет локальный issuer через его
`CheckReadiness` и exact authority ABI 2. При недоступности или ABI mismatch он
закрыто возвращает `stage=delegated_authority`, иначе — `stage=ready`.
Diagnostic намеренно не вызывает policy/credential projection, egress или
provider и не выполняет transcription/provider effect; live provider
проверяется только provider smoke с отдельным owner разрешением.

## Проверка инцидента

1. Сначала разделить local readiness и protected-path stage. Не считать
   `/readyz` подтверждением полного пути.
2. Для `authority|policy|credential` сверять только request/correlation,
   actor/tenant/project locator, source/config revision+digest, provenance,
   account generation и expiry с владельцем состояния. Payload locator не
   является authority.
3. Для `audio` проверить metadata/commit size, media type, bounded chunk и
   checksum без публикации файла. Поддержаны frame-verified MP3 и chunk-verified
   WAV; FLAC закрыто отклоняется.
4. Для `egress` проверять exact proxy/DNS/NetworkPolicy и
   `api.openai.com:443` CONNECT/SNI. Direct egress и TLS weakening запрещены.
5. Для `provider` учитывать, что effect мог начаться. Автоматический retry
   запрещён; новый вызов требует нового authorization context.
6. Success receipt можно сверять по безопасным provenance/config/account/stage
   полям, но transcript в evidence переносить нельзя.

## Provider smoke launcher

`deploy/k8s/base/stt-tts-service-provider-smoke` — неактивный Job с
`backoffLimit: 0`, внешним PVC fixture и Secret file. Перед live effect бинарь
проверяет exact fixture SHA-256
`56a17fd3675e5913e912c404a203bc1062daf3c3c1ec79d5210d20fe28539e8e` и идёт
через тот же egress-gateway. Fixture и credential values в Git отсутствуют.
Job не запускать без отдельного owner OK.

Этот launcher вызывает OpenAI adapter напрямую и не является end-to-end
acceptance. Полная gRPC acceptance обязательна после materialization #1021;
до этого зависимость Issue #1020/#1031 имеет результат `NOT RUN`, а успешный
provider smoke не превращает её в `PASS`.

## Восстановление

Исправления выполняются только в owning unit: policy projection — #1019,
browser/gateway stream — #1021, continuation proof — #1023, credential
projection — #1024/PR #1034. До materialization #1021 STT нельзя добавлять в
active profile, release image set, owner-side ingress или общую certificate
выдачу. Ручные Secret, direct provider egress и ослабление TLS/NetworkPolicy
запрещены. При config publish, account disable/revoke или credential rotation
старый projection закрыто отклоняется по exact revision+digest+generation.
