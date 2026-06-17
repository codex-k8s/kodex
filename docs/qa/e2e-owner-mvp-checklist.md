# Owner MVP E2E Checklist

Этот чек-лист используется перед финальной выдачей owner MVP. Его нужно прогонять на live-контуре после деплоя bot-service. Если во время прохода найдены баги и внесены правки, после нового деплоя чек-лист проходится повторно.

## Автоматический live-прогон 2026-06-17

- [x] `go test ./...` прошел локально.
- [x] `bash -n scripts/remote/e2e-owner-mvp.sh` прошел локально.
- [x] `git diff --check` прошел локально.
- [x] `scripts/remote/install-bot-service.sh --env-file .env --apply --wait` раскатил bot-service и agent-runner.
- [x] `scripts/remote/smoke-bot-service.sh --env-file .env --check-url` прошел на live-контуре.
- [x] Bot-service pod готов, migrations на версии `13`, Mattermost chat listener подключен.
- [x] Full UI preflight прошел: slash menu, button navigation, validation, OpenAI/GitHub accounts, repository onboarding, webhook/check, project dashboard.
- [x] Manager chat e2e прошел: `chat-13-fd7856b56c2b`, status `succeeded`.
- [x] Worker chat e2e прошел: `chat-15-862e049cd499`, status `succeeded`, создан draft PR `codex-k8s/matter-codex-e2e-sandbox#3`.
- [x] Reviewer chat e2e прошел: `chat-15-7ba6c49111c2`, status `succeeded`, GitHub review webhook принят.
- [x] Runtime UI проверен: run list/card, cleanup конкретного run, retention dry-run/apply через confirmation dialog, active retention fixture был пропущен.
- [x] E2E cleanup удаляет временные Mattermost teams/users, matter-codex projects, Kubernetes job/PVC/configmap; PR #3 закрыт, тестовая branch удалена.
- [x] Readback после cleanup: `active_projects=0`, `active_teams=0`, `active_users=0`, `target_runtime_leftovers=0`, `e2e_fixture_leftovers=0`.

## Подготовка

- [x] Ветка собрана и задеплоена через `scripts/remote/install-bot-service.sh --env-file .env --apply --wait`.
- [x] `scripts/remote/smoke-bot-service.sh --env-file .env --check-url` проходит успешно.
- [x] Goose migrations применены на целевой базе без ручного SQL.
- [x] Bot-service pod готов, без restart loop.
- [x] Agent-runner image доступен в Kubernetes runtime.
- [x] В Kubernetes есть хотя бы один авторизованный OpenAI/Codex account secret.
- [x] В Kubernetes есть хотя бы один GitHub account secret с username/email/token.

## Mattermost Menu UX

- [x] `/agents` открывает главное меню.
- [x] Главное меню не показывает typed commands как основной пользовательский путь.
- [x] Переходы `Projects`, `Accounts`, `Repositories`, `Roles`, `Chats`, `Runtime`, `Advanced` работают кнопками.
- [x] Кнопки `Back` и `Main` возвращают к ожидаемым карточкам.
- [x] Успешные dialog submit закрывают модалку и показывают результат отдельной карточкой.
- [x] Ошибки валидации dialog остаются внутри формы и подсвечивают конкретное поле.

## Accounts

- [x] OpenAI account list показывает количество configured/authorized accounts.
- [x] OpenAI account status открывается кнопкой без ввода account name.
- [x] Если device-code истек, account можно снова авторизовать кнопкой.
- [x] Delete OpenAI account работает кнопкой и удаляет Kubernetes auth secret.
- [x] GitHub account list показывает несколько accounts с masked статусом, username/email/scopes.
- [x] GitHub account add/edit/delete доступны через buttons/dialogs без ввода внутренних id.
- [x] GitHub token validation подтягивает username/email через GitHub API.

## Repositories

- [x] Repository onboarding начинается с выбора GitHub account кнопкой.
- [x] Repository search возвращает приватную карточку с результатами.
- [x] Branch selection выбирает branch из GitHub API.
- [x] Repository binding сохраняет provider/owner/name/default branch/account.
- [x] Webhook создается или проверяется при onboarding.
- [x] Repository card имеет check/webhook/edit/delete actions без ручного owner/name.

## Projects

- [x] Project создается из `/agents -> Projects -> Create project`.
- [x] Создание project создает или привязывает Mattermost team.
- [x] Project dashboard показывает team, repositories, roles, chats и linked counters.
- [x] Repository можно привязать к project из dashboard через select.
- [x] Project dashboard дает быстрые кнопки add repo, add role, create chat.

## Roles

- [x] Agent role создается из project dashboard.
- [x] Role editor позволяет выбрать project, role type, OpenAI account, GitHub account.
- [x] Prompt template можно оставить пустым.
- [x] Empty prompt template сохраняет `prompt_mode=raw` и считается валидным режимом.
- [x] Advanced/Codex settings сохраняются в role.
- [x] Kubernetes access mode выбирается явно.
- [x] Role card показывает account bindings и advanced settings summary без секретов.

## Chats

- [x] Chat создается из project dashboard.
- [x] Chat создается как private Mattermost channel внутри project team.
- [x] Chat creator позволяет выбрать manager/pm/worker+reviewer/single/multi custom mode.
- [x] Chat creator позволяет выбрать роли без ввода role id.
- [x] Chat creator позволяет выбрать repository без ввода repository id.
- [x] Chat card показывает roles, repositories, issue context и channel id.
- [x] Bot account состоит в созданном private channel.

## Chat-Triggered Runs

- [x] Сообщение owner в project chat распознается bot-service через Mattermost event listener.
- [x] Сообщение самого bot-service не запускает новый run.
- [x] Если chat неизвестен системе, run не запускается.
- [x] Если role не настроена или OpenAI account отсутствует, owner получает понятную ошибку в thread.
- [x] Если role без prompt template, user message становится основной инструкцией.
- [x] Prompt context содержит project, chat, role, repositories, issue/work policy и locale.
- [x] Manager/PM/ad-hoc role запускается как chat run без обязательного PR.
- [x] Worker role с repository запускает developer run, создает branch и draft PR при изменениях.
- [x] Reviewer role запускает review run, если в сообщении есть GitHub PR URL/number.
- [x] Start message появляется в thread исходного сообщения.
- [x] Final success/failure message появляется в thread исходного сообщения.
- [x] PR URL, branch или review decision попадают в thread/card, если появились в artifacts.

## Runtime

- [x] Runtime menu показывает active/completed/failed runs.
- [x] Run card открывается кнопкой без ввода run id.
- [x] Run card показывает Kubernetes job/pod/status/log tail/artifacts.
- [x] Cleanup конкретного run запускается кнопкой с карточки.
- [x] Retention dry-run показывает skipped active jobs и matched resources.
- [x] Retention apply требует confirmation dialog.
- [x] Held/waiting legacy flows не удаляются retention cleanup без явного owner action.

## Full Dogfooding Scenario

- [x] Создан test project для проверки.
- [x] В project привязан test repository.
- [x] Создана worker role с OpenAI и GitHub account.
- [x] Создан chat с worker role и test repository.
- [x] Owner пишет задачу в chat.
- [x] Agent pod стартует в Kubernetes namespace Mattermost runtime.
- [x] Agent pod получает отдельный PVC.
- [x] Agent pod получает только нужные Kubernetes secrets.
- [x] Agent использует `gh` через выбранный GitHub account.
- [x] Agent использует Codex account через выбранный OpenAI auth secret.
- [x] Agent создает draft PR или сообщает no-changes.
- [x] Thread содержит итоговый ответ агента и ссылку на GitHub entity.
- [x] Runtime cleanup удаляет job/PVC/configmap после проверки.
