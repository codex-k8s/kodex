---
id: EXT-INT-1028-MATRIX
title: Закрытый каталог integration-gateway MVP-UI-42
type: contract-matrix
status: approved
owner: backend
version: 1.0.0
updated: 2026-09-05
---

# Закрытая матрица MVP-UI-42 / #1028

Источник: Issue #1028, Epic #1018 и принятый MVP-UI-42. Эта матрица
фиксирует объём реализации, а не утверждает её готовность.

## Сквозной сценарий

Пользователь/агент вызывает объявленную capability через managed MCP.
Control-plane разрешает user ∩ agent ∩ connection ∩ resource scope,
проверяет активную закреплённую ревизию и Human Gate конкретного эффекта,
назначает invocation/claim и immutable input digest. Gateway получает claim
через существующий generated gRPC client, сверяет shipped definition,
operation/risk/approval, exact scope и input schema **до** чтения credential.
Закрытый adapter вызывает только указанную ниже операцию в одном scope.
Control-plane владеет состоянием invocation, durable receipt, аудитом и
путём чтения результата; gateway не создаёт параллельный источник authority.

Чтение: bounded страница, ограниченный повтор при временной ошибке/rate limit.
Мутация: Human Gate каждого эффекта, один внешний вызов; transport failure,
5xx или повреждённый успешный ответ означают unknown outcome. Повтор команды
не повторяет эффект: решение/reconciliation принадлежит durable lifecycle
control-plane. Отмена/отзыв запрещают новый claim, а не отменяют уже принятый
провайдером эффект. Новых доменных событий этим расширением не вводится;
авторитетный read path остаётся существующим invocation/receipt API.

## Набор операций

В таблице указан суффикс stable operation key после имени провайдера.
Существующие имена сохраняются. Read/list/search не требуют Human Gate;
остальные операции требуют HUMAN_EACH_EFFECT. Delete/merge/cancel относятся
к DESTRUCTIVE, остальные изменения к WRITE или SENSITIVE (запуск workflow/job).

| Провайдер / exact scope | Операции |
| --- | --- |
| GitHub / owner + repository | repository.metadata.read; repository.content.list/read/create/update/delete; branch.list/read/create/delete; commit.list/read; issue.list/read/create/update; issue.comment.list/read/create/update/delete; pull_request.list/read/create/update/merge; pull_request.review.list/read/create; pull_request.file.list; check_run.list/read; actions.workflow.list/read/dispatch; actions.run.list/read/rerun/cancel; actions.job.list/read |
| GitLab / origin + project_path | project.metadata.read; repository.file.read; repository.tree.list; branch.list/read/create/delete; commit.list/read/diff/create; issue.list/read/create/update; issue.note.list/read/create/update/delete; merge_request.list/read/create/update/merge; merge_request.discussion.list/create; merge_request.diff.list; pipeline.list/read/retry/cancel; job.list/read/retry/cancel/trace.read |
| Jira / origin + project_key | project.read; project.user.search/read; issue.search/read/create/update_limited; issue.transition.list/apply; issue.comment.list/read/write/update/delete; issue.link.list/read/write/delete; attachment.list/read/upload/delete |
| Confluence / origin + space_id | space.list/read; page.search/read/create/update; page.descendant.list; page.comment.list/read/create/update/delete; attachment.list/read/upload/delete |
| Synthetic / закреплённый внутренний origin | journal.read/write |

## Протокольные ограничения

- Нет arbitrary URL, метода, тела vendor API или cross-origin redirect.
- Содержимое файлов/вложений ограничено общим бюджетом ответа 64 KiB;
  base64 и JSON также входят в бюджет. Большой файл отклоняется, не обрезается.
- GitHub content update/delete требуют SHA; Confluence update требует version.
- GitHub Actions: workflow/run/job и dispatch/rerun/cancel; загрузка logs/artifacts
  через внешние redirect-хранилища не объявляется.
- Jira users: только assignable users выбранного проекта, не глобальный каталог.
- Jira attachment/link и Confluence child/comment/attachment сначала разрешаются
  через принадлежность точному issue/page/space, не по одному ID из payload.
- Confluence comments: footer comments; inline-редактирование не объявляется.
- Mattermost и email принадлежат отдельным текущим unit и здесь не меняются.

## Доказательство готовности

Каждая строка capability должна иметь исполняемый adapter, schema и отдельный
положительный fake-provider сценарий. Отдельно проверяются exact scope до
credential, denial/revocation/gate, pagination, rate limits, bounded файлы и
unknown mutation без повтора. Итоговый checkpoint содержит фактические команды
и результаты; эта предварительная матрица не заменяет acceptance evidence.
