---
id: RUN-MC-027
title: Диагностика stt-tts-service
type: runbook
status: approved
owner: sre
version: 1.1.0
updated: 2026-09-04
---

# Диагностика stt-tts-service

`stt-tts-service` не входит в shipped profiles до завершения
#1019/#1021/#1023/#1024. Этот runbook описывает неактивный base deployable и не
разрешает его deployment. Не копируйте в evidence audio, transcript, API key,
authority proof/grant или provider body.

## Сигналы

- `/healthz` — процесс отвечает;
- `/readyz` — только local runtime: verifier/config/writable spool;
- `/diagnostics/protected-path` — отдельный JSON readback с bounded полями
  `ready` и `stage`; не является Kubernetes readiness и не вызывает provider;
- `kodex_stt_tts_service_readiness` — local readiness;
- `kodex_stt_tts_service_grpc_requests_total{operation,code}` и
  `kodex_stt_tts_service_grpc_request_duration_seconds{operation}` — unary RPC;
- `kodex_stt_tts_service_transcription_stage_total{stage,error_class}` — один
  итог каждого transcription path. `stage` ограничен
  `authority|policy|audio|credential|egress|provider|success|unknown`,
  `error_class` —
  `none|denied|invalid|unavailable|timeout|rejected|unknown`.

Лог `unexpected gRPC failure` содержит только bounded `method` и canonical
`code`; edge local readiness — только `error_class=local_runtime`. Request и
correlation доступны в success receipt, но намеренно не логируются.

## Разделение readiness и protected path

Успешный `/readyz` означает лишь способность Pod безопасно принять RPC. Он не
проверяет и не обещает доступность control-plane, secret-broker, egress-gateway
или OpenAI. В текущей ревизии `/diagnostics/protected-path` закрыто возвращает
`stage=delegated_authority`: approved primitive server-owned continuation proof
ещё не материализован #1023, поэтому оба projection adapter останавливаются до
сетевого RPC.

После materialization последовательно диагностируются policy, credential,
egress и provider. Diagnostic не должен выполнять transcription/provider
effect; live provider проверяется только acceptance harness с отдельным owner
разрешением.

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

## Acceptance launcher

`deploy/k8s/base/stt-tts-service-acceptance` — неактивный Job с
`backoffLimit: 0`, внешним PVC fixture и Secret file. Перед live effect бинарь
проверяет exact fixture SHA-256
`56a17fd3675e5913e912c404a203bc1062daf3c3c1ec79d5210d20fe28539e8e` и идёт
через тот же egress-gateway. Fixture и credential values в Git отсутствуют.
Job не запускать без отдельного owner OK.

## Восстановление

Исправления выполняются только в owning unit: policy projection — #1019,
browser/gateway stream — #1021, continuation proof и authority profile —
#1023, credential projection — #1024. До materialization STT нельзя добавлять
в active profile, release image set, owner-side ingress или общую certificate
выдачу. Ручные Secret, direct provider egress и ослабление TLS/NetworkPolicy
запрещены.
