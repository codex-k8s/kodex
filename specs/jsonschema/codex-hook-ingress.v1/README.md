# codex-hook-ingress v1 JSON Schema

## Назначение

Этот каталог фиксирует CHI-1 machine-readable contract для normalized Codex hook envelope и sanitizer contract.

Схемы не являются transport contract: `proto`, OpenAPI или AsyncAPI для `SubmitHookEvent` выбираются отдельным срезом. JSON Schema описывает безопасную форму данных, которую hook emitter или local sidecar может передать в `codex-hook-ingress`, а ingress может маршрутизировать владельцам.

## Файлы

| Файл | Назначение |
|---|---|
| `normalized-hook-envelope.v1.schema.json` | JSON Schema нормализованного hook envelope для MVP events. |
| `sanitizer-contract.v1.schema.json` | JSON Schema политики очистки: лимиты, forbidden fields, redaction, digest/preview и downstream safe parts. |
| `sanitizer-contract.defaults.json` | Стартовый экземпляр sanitizer contract, валидируемый схемой. |
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
```

Эта проверка валидирует machine-readable форму схем и safe examples. Она не генерирует Go-код: CHI-1 не создаёт transport contract и сервисный каркас.
