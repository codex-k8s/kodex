---
id: RUN-MC-012
title: Диагностика integration-gateway
type: runbook
status: approved
owner: sre
version: 3.3.0
updated: 2026-08-29
---

# Диагностика integration-gateway

## Штатное пустое состояние

Gateway обязан стартовать и быть Ready при нуле connections и credentials.
Shipped definitions загружаются локально и сверяются по версии/digest с
control-plane. В каталог входят GitHub, GitLab, Jira, Confluence, электронная
почта, synthetic fixture и Mattermost. Ни один внешний provider не является
startup dependency. UI показывает только definitions из server readback:
отсутствующий package не подменяется локальной placeholder-карточкой.

## MCP admission

Runtime вызывает только зарегистрированный typed MCP tool. Для invocation
проверяются exact:

- organization/project/session/turn/attempt и immutable input digest;
- IntegrationDefinition version/origin/digest и capability operation;
- active Connection metadata и credential revision без secret value;
- server-owned Agent/Workflow grant;
- risk и наличие явного grant;
- application grant, mTLS peer, method, fence и replay watermark.

Gateway не предоставляет universal HTTP/API proxy. Provider credentials
остаются в Kubernetes Secret, смонтированном только в `integration-gateway`;
role Pod получает только session-scoped MCP binding. Raw provider response не
выходит в audit/event/PWA.

## Probes

`/healthz` проверяет процесс, `/readyz` читает локальный снимок issuer sidecar.
Control-plane, credential конкретного Connection, egress gateway и external
providers не вызываются на Kubernetes probe. Их фактическая доступность
проверяется рабочим invocation либо отдельной пользовательской операцией test.

## Effect и grant

Один provider effect связывается с exact invocation/attempt/fence, definition
digest, resource scope, input digest и effect key. READ может перейти к claim
без согласования. WRITE, SENSITIVE и DESTRUCTIVE создают отдельный Human Gate
до claim; credential не выдаётся worker до `APPROVED`. После durable completion
receipt тот же invocation не исполняется повторно. Exact повтор завершения
возвращает readback одной receipt, несовпадающий повтор закрыто отклоняется.

## Credential revision

Connection хранит только `secret_ref`, Secret UID, `resourceVersion`, revision
и SHA-256 содержимого. Control Center сначала создаёт Connection без credential,
а затем отдельной OCC-командой передаёт значение в `control-plane`.
`control-plane` имеет `get`/`update` только на Secret
`kodex-integration-credentials`, создаёт детерминированный data key и немедленно
очищает копию значения в памяти. Browser не назначает Secret UID,
`resourceVersion`, ref или digest. Для shipped GitHub package допустим только
ref вида `kodex-system/kodex-integration-credentials#integration-<digest>`.
Gateway сверяет все metadata, читает ровно указанный key из root-mounted Secret
и проверяет digest перед созданием provider client. В connection config,
публичном readback, PostgreSQL, логах и документации token value отсутствует.

## Exact egress

GitHub adapter имеет фиксированный endpoint `https://api.github.com/` и идёт
только через `egress-gateway.kodex-system.svc.cluster.local:8080`. GitLab,
Jira, Confluence и email bridge получают exact HTTPS origin из проверенного
Connection config и используют тот же proxy. Shared policy должна разрешать
каждый exact FQDN на `443`; wildcard и разрешение произвольного Internet egress
запрещены. Runtime NetworkPolicy разрешает только pod `egress-gateway` с
component `platform-egress` на `8080/TCP`.
Synthetic adapter принимает только
`integration-synthetic.kodex-system.svc.cluster.local:8080`, а NetworkPolicy —
только pod labels `integration-synthetic`/`integration-fixture`. Redirect
запрещён.

`networkDestinations` package является декларацией требуемой границы, но не
расширяет egress policy само по себе. Перед активацией Connection SRE добавляет
exact hostname из approved config в materialized policy и проверяет итоговый
render. Пока этого не сделано, операция test обязана вернуть безопасный
`INTEGRATION_UNAVAILABLE`.

## Shipped provider profiles

| Package | Read path | Effects после Human Gate | Особенности |
| --- | --- | --- | --- |
| GitHub | repository metadata | create/update issue | fixed `api.github.com`, exact owner/repository |
| GitLab | project, file, issue, merge request, pipeline | issue, note, branch, commit, merge request, retry pipeline | REST v4 от configured HTTPS origin |
| Jira | project, bounded issue search/read | create, comment, limited update, link | Cloud REST v3, BASIC или BEARER; текст материализуется в ADF |
| Confluence | space, bounded page search/read | draft create, OCC update | Cloud REST v2; update требует exact expected version |
| Email | bridge health | text message send | HTTPS bridge обязан поддерживать native idempotency key |
| Synthetic | journal read | journal write | только disposable local E2E |
| Mattermost | interaction capabilities | interaction effects | выполняет optional `interaction-gateway` |

Для Jira и Confluence при BASIC auth `username` обязателен и credential value
содержит API token. При BEARER `username` не используется. GitLab принимает
token как bearer credential. UI и API никогда не возвращают credential value;
диагностика показывает только masked state, время и локализованный safe outcome
последней проверки.

## Изолированный GitHub fixture

Live fixture не входит в локальный baseline и запускается parent после
integration. Его безопасная конфигурация передаётся только окружением тестовой
оснастки:

```text
KODEX_INTEGRATION_E2E_GITHUB_OWNER=codex-k8s
KODEX_INTEGRATION_E2E_GITHUB_REPOSITORY=kodex-integration-e2e
KODEX_GITHUB_BOT_PAT=<Kubernetes Secret source only>
```

Repository private; `kodex-agent` имеет pull/push без admin. Значение
`KODEX_GITHUB_BOT_PAT` не включается в package, Connection, manifest, command
line или отчёт. Двухфазная команда Control Center создаёт/обновляет data key в
`kodex-integration-credentials`, после чего Connection получает только
authoritative Secret metadata и content digest. В production fixture owner и
repository не имеют default и всегда задаются Connection config.

## Поддерживаемый deployed local E2E

После #1028 crash-window mutation больше не возвращается в очередь:
просроченная mutation lease становится `UNKNOWN_OUTCOME`. Старый deployed
replay-сценарий ниже описывает прежнюю семантику и не является acceptance
нового профиля. Он не запускался в рамках #1028. Актуальные targeted
сценарии и запрет повторного эффекта описаны в
[`OPS-INT-1028`](../operations/integration-gateway-1028.md).

Отдельный entrypoint `scripts/tests/integration-deployed-e2e.sh` проверяет
развёрнутый local path, не подменяя `integration-gateway` прямым provider
вызовом. Сценарий создаёт одноразовые Project, Agent и Connection через
Control API, запускает обычный agent Run и требует вызов typed
`invoke_integration` через runtime MCP boundary.

Synthetic-профиль выполняет:

1. READ без Human Gate и без изменения journal;
2. WRITE с `REJECT`, после которого provider count остаётся равен нулю;
3. WRITE с `APPROVE`, после provider effect принудительно пересоздаёт только
   local Pod `integration-gateway`;
4. ждёт истечения 30-секундной invocation lease и повторного exact claim;
5. сверяет local-only diagnostic readback: `count=1`, `replay_count>=1` и
   одинаковый last effect/replay key.

Прямая local fixture ручка используется только для readback. Запись в journal
через неё запрещена тестовым сценарием. Специальное значение с префиксом
`kodex-e2e-replay:` задерживает только первый ответ local-only fixture на четыре
секунды, чтобы воспроизводимо создать crash window после durable provider
effect. Повтор с тем же effect key отвечает немедленно.

Перед запуском требуются готовый local namespace и установленные зависимости
Control Center:

```bash
cd services/staff/control-center
npm ci --ignore-scripts
cd ../../..

export KODEX_E2E_KUBECONFIG='/absolute/path/to/local-kubeconfig'
export KODEX_E2E_BASE_URL='https://<trusted-local-origin>'
export KODEX_E2E_OWNER_USERNAME='<local-owner>'
export KODEX_E2E_OWNER_PASSWORD='<owner-private-value>'
export KODEX_E2E_RESOURCE_PREFIX='integration-unique-slug'
export KODEX_E2E_CONFIRM_DISPOSABLE='I_UNDERSTAND_THIS_MUTATES_A_DISPOSABLE_INSTALLATION'
make test-integration-deployed-e2e
```

Значения credentials не передаются в аргументах процессов. Auth storage,
Playwright output и port-forward log создаются в owner-private temporary
directory и удаляются по `trap`; screenshot, trace, video и HTML report
отключены. Для одной установки каждый запуск использует новый
`KODEX_E2E_RESOURCE_PREFIX`, потому что Project является audit evidence и не
удаляется сценарием.

GitHub-профиль выключен по умолчанию. Он включается только явным
`KODEX_INTEGRATION_E2E_GITHUB=1`, всегда ограничен private repository
`codex-k8s/kodex-integration-e2e` и требует ровно один источник token:

```bash
export KODEX_INTEGRATION_E2E_GITHUB=1
export KODEX_GITHUB_BOT_PAT_FILE='/absolute/owner-private/path/kodex-agent.token'
make test-integration-deployed-e2e
```

Вместо файла допускается owner-private environment
`KODEX_GITHUB_BOT_PAT`; одновременно задавать оба источника запрещено. Файл
должен быть regular non-symlink, принадлежать текущему UID и не иметь прав для
group/other. Token передаётся в двухфазную credential-команду платформы и не
попадает в argv, Connection readback, reporter или artifacts.

GitHub-сценарий читает metadata exact private repository, затем через
`invoke_integration` создаёт ровно один issue с уникальным marker после Human
Gate. Прямой GitHub API применяется только для authoritative before/after
readback и cleanup: созданный issue закрывается в `finally`. `gh` и прямой API
не используются как доказательство платформенного write.

Статическая проверка TypeScript и обнаружения отдельного suite выполняется без
мутаций:

```bash
make test-integration-deployed-e2e-check
```

## Диагностика

| Safe code | Действие |
|---|---|
| `INTEGRATION_CONFIGURATION_INVALID` | сверить package fields, exact scope и input digest |
| `INTEGRATION_CREDENTIAL_UNAVAILABLE` | сверить Secret ref/UID/resourceVersion/digest, не читать value в диагностике |
| `INTEGRATION_AUTH_REJECTED` | проверить права token на exact repository |
| `INTEGRATION_RATE_LIMITED` | дождаться provider retry window, не обходить receipt |
| `INTEGRATION_UNAVAILABLE` | проверить exact egress host/SNI и provider status |
| `INTEGRATION_REQUEST_REJECTED` | проверить typed input и provider resource state |
| `INTEGRATION_RESPONSE_INVALID` | считать effect неизвестным и сверить authoritative provider state/receipt |
| `INTEGRATION_CAPABILITY_UNSUPPORTED` | сверить package version/digest и наличие закрытого adapter operation |
| replay или fence conflict | не повторять вручную; сверить authoritative receipt |

Secret values, provider bodies и MCP bearer не печатаются. Ошибка возвращает
stable key; локализованный пользовательский текст находится в YAML i18n.

Frontend и browser E2E проверяются parent-волной #992. Локальная проверка
использует отдельный private fixture repository и bot token с минимальными
правами; production repository и credentials в этот контур не входят.

## Ограничения live-проверки

- Без test credentials и approved exact egress hosts выполняются schema, unit и
  synthetic contract checks, но не provider live calls.
- GitLab `base_url` в текущем профиле является origin; installations под URL
  prefix не поддерживаются.
- Jira и Confluence проверены по Cloud REST contracts. Отличающиеся Server/Data
  Center API требуют отдельного package/adapter version.
- Email package не является SMTP client: нужен HTTPS bridge с контрактом
  `/v1/health` и `/v1/messages`.
- Provider mutation с неизвестным сетевым исходом не повторяется вручную:
  сначала сверяется authoritative provider state и invocation receipt.
