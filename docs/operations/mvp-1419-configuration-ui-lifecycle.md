---
id: OPS-DOC-1419
title: Приёмка UI lifecycle шаблонов и IntegrationDefinition
type: operations
status: approved
owner: sre
version: 1.0.0
updated: 2026-09-09
---

# Приёмка UI lifecycle шаблонов и IntegrationDefinition

Профиль закрывает воспроизводимый staging-путь создания собственных
`PROMPT_TEMPLATE` и `INTEGRATION_DEFINITION` без Git. Это дополнительное
доказательство для `MVP-UI-15`, `MVP-UI-30`, `CFG-02` и применимой части
`CFG-03`; оно не закрывает остальные варианты этих строк.

## Граница

Все изменения выполняет браузер через реальные формы Control Center. API-only
сессия используется только для авторитетного `GET` readback. Профиль не создаёт
connections/grants, не запускает provider, Run, STT или email и не изменяет
чужие объекты. Он использует только уникальные имена с operator-owned prefix.

`PROMPT_TEMPLATE` проходит create, validate, publish и history. Forward restore,
copy и archive у этого вида отсутствуют в текущем специализированном UI/API и
не отмечаются как выполненные. `INTEGRATION_DEFINITION` проходит Form/YAML,
create, validate, publish, history, создание нового forward draft из выбранной
опубликованной revision, copy и archive собственной копии. Restore не меняет
published pointer и не перепривязывает consumers.

## Безопасные входы

Для live-запуска нужны:

- отдельный API-only `storageState`, подготовленный штатным setup;
- exact HTTPS origin;
- SHA исходников harness, API и PWA;
- приватный serving manifest и его SHA-256;
- новый приватный journal path в каталоге `0700`;
- уникальный prefix длиной 4–40 символов;
- tracked `contracts/integrations/v1/definitions/synthetic.yaml`.

Журнал имеет mode `0600`. Перед каждой UI mutation он синхронно записывает
`intent` и вызывает `fsync`. В нём остаются только закрытый operation, sequence,
HTTP status, версии и SHA-256 входа/request/ref/published pointer. Исходники,
prompt, cookies, CSRF, headers, ответы и персональные данные не сохраняются.

## Запуск

Сначала получить отдельную API-only сессию штатным `api-session` setup. Затем
из точного checkout выполнить:

```bash
KODEX_E2E_CONFIGURATION_LIFECYCLE_CONFIRM=RUN_CONFIGURATION_UI_LIFECYCLE \
KODEX_E2E_CONFIGURATION_LIFECYCLE_STATE=/absolute/private/new-state.jsonl \
KODEX_E2E_SERVING_MANIFEST=/absolute/private/serving-manifest.json \
KODEX_E2E_STORAGE_STATE=/absolute/private/api-session.json \
KODEX_E2E_RESOURCE_PREFIX=<unique-prefix> \
KODEX_E2E_BASE_URL=https://control.kodex.works \
KODEX_E2E_SOURCE_REVISION=<40-hex> \
KODEX_E2E_API_REVISION=<40-hex> \
KODEX_E2E_PWA_REVISION=<40-hex> \
KODEX_E2E_SERVING_MANIFEST_SHA256=<64-hex> \
cd services/staff/control-center
npx playwright test --config e2e/configuration-lifecycle.config.ts
```

`workers=1`, `retries=0`, trace/video/screenshot выключены. При неизвестном
исходе команда не повторяется. Для отдельного readback того же journal:

```bash
KODEX_E2E_CONFIGURATION_LIFECYCLE_RESUME=1 \
<те же exact scope variables> \
cd services/staff/control-center
npx playwright test --config e2e/configuration-lifecycle.config.ts
```

Resume выполняет только `GET`, сопоставляет точное deterministic имя,
kind/state/version/published pointer и завершает pending intent как `PASS` либо
`UNKNOWN`. Для продолжения mutation после подтверждения оператор создаёт новый
уникальный запуск и prefix; автоматического повтора после `UNKNOWN` нет.

## Исходы

- `PASS`: принят HTTP command и совпал авторитетный readback либо resume нашёл
  состояние, однозначно доказывающее исход.
- `REJECTED`: получен terminal HTTP 4xx; следующий intent не создаётся.
- `UNKNOWN`: нет ответа или HTTP 5xx, либо readback не доказывает эффект.
- `NOT RUN`: live-профиль не запускался на обслуживаемом staging candidate.

Платный или внешний provider effect всегда `NOT RUN` в этом профиле.
