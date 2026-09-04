# STT API

Модуль содержит сгенерированный Go-контракт `stt.v1`. Источник истины —
`contracts/proto/stt/v1/stt.proto`; файлы в `gen/` вручную не изменяются.

`SpeechToTextService` принадлежит `stt-tts-service`. Контракты проекций
реализуются владельцами состояния: `control-plane` в #1019 и `secret-broker` в
#1024. Они не являются публичным browser API.
