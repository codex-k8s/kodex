---
id: RUN-MC-015
title: Локальная проверка типизированных интеграций
type: runbook
status: approved
owner: qa
version: 2.0.0
updated: 2026-08-29
---

# Локальная проверка типизированных интеграций

Локальный E2E использует единственный канонический путь Kodex:

`integration package -> connection -> typed grant -> RuntimeRevision -> MCP -> integration-gateway -> provider -> effect receipt`.

Нейтральным provider является `integration-synthetic` из
`deploy/k8s/base/integration-synthetic`. Он устанавливается только локальным
overlay `deploy/k8s/overlays/local/integration-synthetic` и не входит в
production profiles. Отдельного standalone API с собственными approval,
idempotency или retry semantics нет: такие проверки обязаны проходить через
авторитетные lifecycle Kodex и Human Gate.

## Что доказывает сценарий

1. Synthetic package доступен в реестре definitions.
2. Connection создаётся, проходит `TEST`, отключается и включается обратно.
3. Устаревший `If-Match` закрыто отклоняется до изменения состояния.
4. ИИ-сотруднику выдаются только `synthetic.journal.read` и
   `synthetic.journal.write`.
5. READ проходит через `invoke_integration` без побочного эффекта.
6. WRITE не достигает provider до решения Human Gate; `REJECT` не создаёт
   effect, `APPROVE` разрешает ровно один effect.
7. Намеренно задержанный provider response приводит к повторной доставке после
   restart `integration-gateway`, но exact effect key не создаёт второй effect.
8. Run events содержат единственный typed tool call и непустой audit ref.

## Статические и render-проверки

```bash
./scripts/tests/integration-synthetic-test.sh
```

Команда запускает Go race tests fixture/adapter, рендерит local Kustomize,
проверяет security context, resources, exact ingress и отсутствие synthetic
provider в production profiles.

```bash
cd services/staff/control-center
npm run typecheck
KODEX_E2E_CHECK_ONLY=1 \
  ./node_modules/.bin/playwright test \
  --config playwright.integration.config.ts --list
```

## Deployed local E2E

Требуется уже развёрнутый disposable local k3s-контур с
`integration-synthetic` и авторизованным owner. Значения credentials не
передаются параметрами команд и не записываются в Git.

```bash
export KODEX_E2E_KUBECONFIG=/absolute/path/to/local-kubeconfig
export KODEX_E2E_BASE_URL=https://local-kodex.example.test
export KODEX_E2E_OWNER_USERNAME=owner
export KODEX_E2E_OWNER_PASSWORD='<получить из локального secret manager>'
export KODEX_E2E_RESOURCE_PREFIX=local-integration-$(date +%s)
export KODEX_E2E_CONFIRM_DISPOSABLE=I_UNDERSTAND_THIS_MUTATES_A_DISPOSABLE_INSTALLATION
./scripts/tests/integration-deployed-e2e.sh
```

Значение `KODEX_E2E_OWNER_PASSWORD` передаётся процессу из локального secret
manager и не записывается в shell history, `.env`, Playwright artifacts или
Git.

## Опциональный GitHub E2E

GitHub-проверка использует отдельный private test repository и проходит через
тот же typed integration path. Repository заранее создаётся владельцем или
оператором; тест не создаёт и не удаляет repository. Он читает metadata,
создаёт одну Issue после Human Gate, выполняет authoritative readback и в
`finally` закрывает только Issue с уникальным E2E marker.

Обычные, несекретные параметры:

```bash
export KODEX_INTEGRATION_E2E_GITHUB=1
export KODEX_INTEGRATION_E2E_GITHUB_OWNER=example-owner
export KODEX_INTEGRATION_E2E_GITHUB_REPOSITORY=kodex-integration-e2e
```

Credential задаётся ровно одним способом:

```bash
export KODEX_GITHUB_BOT_PAT_FILE=/absolute/private/path/github-token
# или KODEX_GITHUB_BOT_PAT через локальный secret manager/процесс запуска
```

Token file должен быть обычным owner-private файлом, не symlink, с mode без
group/other bits. Токен не печатается, не попадает в Playwright artifacts,
создаваемую connection metadata или Git. Test repository должен быть private,
а credential — принадлежать отдельному bot account с минимальными правами на
metadata и Issues только этого repository.

## Ручная диагностика

```bash
export KUBECONFIG=/absolute/path/to/local-kubeconfig
kubectl -n kodex-system get deployment/integration-synthetic
kubectl -n kodex-system get networkpolicy \
  -l app.kubernetes.io/name=integration-synthetic
kubectl -n kodex-system logs deployment/integration-synthetic
kubectl -n kodex-system port-forward service/integration-synthetic 18082:8080
curl --fail --silent http://127.0.0.1:18082/readyz
```

Логи и ответы synthetic provider содержат только synthetic identifiers и
безопасные значения. Playwright screenshot, trace и video для API readback не
требуются.

APIRequestContext, response assertions и conditional skip сверены по
документации Playwright 1.61 через Context7
(`/microsoft/playwright/v1.61.0`).
