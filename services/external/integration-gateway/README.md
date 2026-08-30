---
id: EXT-MC-003
title: Integration gateway
type: service
status: approved
owner: backend
version: 2.2.0
updated: 2026-08-29
---

# integration-gateway

`integration-gateway` — stateless worker типизированных внешних capabilities.
Метаданные подключений, grants, leases, результаты и audit принадлежат
`control-plane`. Пустой набор подключений является штатным состоянием и не
влияет на readiness.

Юнит не предоставляет универсальный proxy. Один schema-versioned YAML package
определяет adapter, configuration fields, capabilities, operation, risk,
approval policy, input и resource scope. Gateway принимает только совпадающие
`definition_version`, `definition_digest`, grant scope и immutable input
digest.

Поставляются семь schema-versioned packages:

- synthetic HTTP journal: read и идемпотентный write по exact effect key;
- GitHub: repository metadata read и create/update issue только в exact
  `owner/repository` Connection scope через `https://api.github.com`;
- GitLab: metadata, repository file, issues, merge requests, branches, commit и
  pipeline в exact `base_url/project_path` scope;
- Jira: project, bounded issue search/read, create, comment, limited update и
  issue link в exact `base_url/project_key` scope;
- Confluence: space, bounded page search/read, draft create и OCC update в exact
  `base_url/space_id` scope;
- электронная почта: health и отправка текстового письма через
  provider-neutral HTTPS bridge с provider-native idempotency;
- Mattermost остаётся за отдельным необязательным `interaction-gateway`.

Package также объявляет типизированные output fields, exact network
destinations и health operation. Универсального HTTP passthrough нет: неизвестная
операция, поле, adapter, resource scope или provider response отклоняется до
выдачи результата.

Credential claim содержит только revision ref, Kubernetes Secret
`namespace/name#key`, Secret UID, `resourceVersion` и content SHA-256. Credential
читается из server-mounted Secret непосредственно перед provider-вызовом,
проверяется по digest и не возвращается в API, логи, audit или result.

Все внешние HTTPS-вызовы идут через `egress-gateway`. Configured `base_url`
принимается только как HTTPS origin без userinfo, query, fragment, IP literal и
нестандартного порта. Оператор установки обязан материализовать каждый exact
FQDN в policy egress gateway; отсутствие host в policy является штатным
fail-closed отказом подключения. Redirect запрещён.

READ повторяется только на bounded network/`429`/`502`/`503`/`504` отказах.
Provider mutation без нативной идемпотентности автоматически не повторяется
после неоднозначного сетевого исхода; immutable invocation receipt защищает от
повторного выполнения уже подтверждённого effect. Email bridge обязан принимать
`Idempotency-Key` и возвращать один provider receipt для exact retry.

READ invocation может быть claim-нут сразу. WRITE, SENSITIVE и DESTRUCTIVE
сначала атомарно создают отдельный Human Gate и остаются недоступны worker до
`APPROVED`. Успешный внешний effect завершается immutable receipt с exact
effect key, input digest, provider effect ref и response digest; повторное
завершение допустимо только как exact readback той же receipt.

`/healthz` отражает жизнь процесса, `/readyz` читает локальный снимок sidecar
authority. Доступность control-plane и внешних систем наблюдается отдельным
рабочим/diagnostic контуром и не меняет Kubernetes readiness pod.

## Disposable synthetic fixture

Бинарь `cmd/integration-synthetic` является только локальной E2E-оснасткой и
не входит в `web-only`, `web-with-mattermost`, staging или production render.
`tools/dev/render-local.sh` добавляет его отдельным overlay в `kodex-system` и
запускает через общий hot-reload runner.

Fixture поддерживает только закрытый контракт:

- `GET /healthz` и `GET /readyz`;
- `GET /v1/journals/{journal}` без изменения состояния;
- `POST /v1/journals/{journal}/entries` со строгим JSON `{"value":"..."}` и
  обязательным `Idempotency-Key`.

Journal ограничен 120 байтами, value — 4096 байтами, body — 8 KiB. Неизвестные,
повторяющиеся или дополнительные JSON fields отклоняются. Один ключ и тот же
request возвращают сохранённый provider readback; тот же ключ с другим journal
или value получает `409` без эффекта. Состояние ограничено, потокобезопасно и
существует только в течение жизни одного disposable процесса.

`make test-integration-synthetic` выполняет race-тесты fixture и synthetic
adapter, проверяет exact local NetworkPolicy и доказывает отсутствие Deployment
в release profiles. PostgreSQL component test `TestBootstrapComponent` содержит
lifecycle-сценарии READ без gate, WRITE до Human Gate без claim, REJECT без
effect receipt, APPROVE с одной receipt и exact retry/readback без нового claim.
