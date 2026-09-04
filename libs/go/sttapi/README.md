# STT API

Модуль содержит сгенерированный Go-контракт `stt.v1`. Источник истины —
`contracts/proto/stt/v1/stt.proto`; файлы в `gen/` вручную не изменяются.

`SpeechToTextService` принадлежит `stt-tts-service`. Контракты проекций
реализуются владельцами состояния: `control-plane` в #1019 и `secret-broker` в
#1024. Они не являются публичным browser API. Оба projection RPC используют
единый `DelegatedAuthorityLocator` только как locator/echo; полномочия обязан
доказывать server-owned delegated/continuation proof из #1023. До появления
этого primitive producer закрыто отказывает до сетевого RPC.

`Transcribe` — client-streaming RPC: metadata предшествует bounded chunks,
commit фиксирует точный размер и SHA-256. Success возвращает transcript и
безопасный provenance receipt без audio, credential или authority grant.
`CheckReadiness` относится только к локальному runtime, а
`CheckProtectedPath` — отдельный diagnostic readback и не Kubernetes
readiness.
