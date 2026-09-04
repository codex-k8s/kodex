---
id: RUN-MC-027
title: Диагностика stt-tts-service
type: runbook
status: approved
owner: sre
version: 1.0.0
updated: 2026-09-04
---

# Диагностика stt-tts-service

`stt-tts-service` выполняет stateless STT через OpenAI и не хранит аудио,
transcript или credential. Не копируйте эти значения в evidence инцидента.

## Probes и метрики

- `/healthz` подтверждает жизнь процесса;
- `/readyz` проверяет полный локальный authority/projection path без provider
  effect;
- `kodex_stt_tts_service_readiness` отражает готовность;
- `kodex_stt_tts_service_grpc_requests_total` и
  `kodex_stt_tts_service_grpc_request_duration_seconds` имеют только закрытые
  `operation` и canonical gRPC `code`.

## Проверка инцидента

1. По безопасному correlation ID определить gRPC code и один из этапов:
   authority, policy projection, credential projection, локальная audio
   validation, exact egress или OpenAI.
2. При неготовности проверить соответствующие `control-plane` вместе с его
   proof resolver, `secret-broker` и локальные issuer/verifier. Пустые UDS или
   application-grant slots означают, что prerequisite #1023 не
   материализован.
3. Сверить только revision/digest/generation/expiry metadata. Не выводить
   application grant, API key, request audio, transcript или provider body.
4. Для `InvalidArgument`/`ResourceExhausted` проверить media type, magic bytes,
   размер и локально вычисленную длительность. Не отправлять файл в сторонние
   диагностические сервисы.
5. Для `Unavailable` проверить DNS к exact egress proxy, policy
   `api.openai.com:443`, CONNECT/SNI и TLS certificate validation. Direct
   egress и изменение proxy URL запрещены.
6. Повторять запрос автоматически нельзя: внешний effect мог начаться. Новый
   вызов получает новый authority context и credential projection.

## Восстановление

Исправление выполняется forward-only в owning unit: config/permission — #1019,
credential projection — #1024, authority profiles — #1023, HTTP boundary —
#1021, egress policy — `egress-gateway`. Ручное создание Secret, копирование
ключа в env или ослабление TLS/NetworkPolicy запрещены.
