# codex-hook-ingress v1 JSON Schema

## Назначение

Этот каталог фиксирует CHI-1/CHI-2 machine-readable contract для normalized Codex hook envelope, sanitizer contract и runtime config hook emitter/local sidecar.

Схемы не являются transport contract: `proto`, OpenAPI или AsyncAPI для `SubmitHookEvent` выбираются отдельным срезом. JSON Schema описывает безопасную форму данных и runtime policy, которую hook emitter или local sidecar должен соблюдать до передачи envelope в `codex-hook-ingress`.

## Файлы

| Файл | Назначение |
|---|---|
| `normalized-hook-envelope.v1.schema.json` | JSON Schema нормализованного hook envelope для MVP events. |
| `sanitizer-contract.v1.schema.json` | JSON Schema политики очистки: лимиты, forbidden fields, redaction, digest/preview и downstream safe parts. |
| `sanitizer-contract.defaults.json` | Стартовый экземпляр sanitizer contract, валидируемый схемой. |
| `hook-emitter-config.v1.schema.json` | JSON Schema runtime config для hook emitter/local sidecar: input, delivery, auth, idempotency, ordering, retry, buffer и failure policy. |
| `hook-emitter-config.defaults.json` | Стартовый экземпляр runtime config CHI-2, валидируемый схемой. |
| `examples/session-start.safe.json` | Safe envelope для `SessionStart`. |
| `examples/user-prompt-submit.safe.json` | Safe envelope для `UserPromptSubmit`. |
| `examples/pre-tool-use.safe.json` | Safe envelope для `PreToolUse`. |
| `examples/permission-request.safe.json` | Safe envelope для `PermissionRequest`. |
| `examples/post-tool-use.safe.json` | Safe envelope для `PostToolUse`. |
| `examples/stop.safe.json` | Safe envelope для `Stop`. |

## Проверка

Рекомендуемая локальная проверка:

```bash
npx --yes ajv-cli@5.0.0 validate --spec=draft2020 \
  -s specs/jsonschema/codex-hook-ingress.v1/normalized-hook-envelope.v1.schema.json \
  -d 'specs/jsonschema/codex-hook-ingress.v1/examples/*.safe.json'

npx --yes ajv-cli@5.0.0 validate --spec=draft2020 \
  -s specs/jsonschema/codex-hook-ingress.v1/sanitizer-contract.v1.schema.json \
  -d specs/jsonschema/codex-hook-ingress.v1/sanitizer-contract.defaults.json

npx --yes ajv-cli@5.0.0 validate --spec=draft2020 \
  -s specs/jsonschema/codex-hook-ingress.v1/hook-emitter-config.v1.schema.json \
  -d specs/jsonschema/codex-hook-ingress.v1/hook-emitter-config.defaults.json
```

Эта проверка валидирует machine-readable форму схем, defaults и safe examples. Она не генерирует Go-код: CHI-1/CHI-2 не создают transport contract и сервисный каркас.
