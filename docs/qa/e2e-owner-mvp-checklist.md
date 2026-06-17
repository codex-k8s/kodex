# Owner MVP E2E Checklist

Этот чек-лист используется перед финальной выдачей owner MVP. Его нужно прогонять на live-контуре после деплоя bot-service. Если во время прохода найдены баги и внесены правки, после нового деплоя чек-лист проходится повторно.

## Автоматический live-прогон 2026-06-17

- [x] `go test ./...` прошел локально.
- [x] `bash -n scripts/remote/e2e-owner-mvp.sh` прошел локально.
- [x] `git diff --check` прошел локально.
- [x] `scripts/remote/install-bot-service.sh --env-file .env --apply --wait` раскатил bot-service и agent-runner.
- [x] `scripts/remote/smoke-bot-service.sh --env-file .env --check-url` прошел на live-контуре.
- [x] Bot-service pod готов, migrations на версии `13`, Mattermost chat listener подключен.
- [x] Manager chat e2e прошел после исправлений: `chat-5-c5609753d512`, status `succeeded`.
- [x] Worker/no-changes e2e прошел после исправлений: `chat-6-7ca66c1a18b5`, status `succeeded`, PR не создавался.
- [x] E2E cleanup удаляет временные Mattermost teams/users, matter-codex projects, Kubernetes job/PVC/configmap.
- [x] Readback после cleanup: `projects=0`, `active_teams=0`, `active_users=0`, runtime leftovers отсутствуют.

## Подготовка

- [x] Ветка собрана и задеплоена через `scripts/remote/install-bot-service.sh --env-file .env --apply --wait`.
- [x] `scripts/remote/smoke-bot-service.sh --env-file .env --check-url` проходит успешно.
- [x] Goose migrations применены на целевой базе без ручного SQL.
- [x] Bot-service pod готов, без restart loop.
- [x] Agent-runner image доступен в Kubernetes runtime.
- [x] В Kubernetes есть хотя бы один авторизованный OpenAI/Codex account secret.
- [x] В Kubernetes есть хотя бы один GitHub account secret с username/email/token.

## Mattermost Menu UX

- [ ] `/agents` открывает главное меню.
- [ ] Главное меню не показывает typed commands как основной пользовательский путь.
- [ ] Переходы `Projects`, `Accounts`, `Repositories`, `Roles`, `Chats`, `Runtime`, `Advanced` работают кнопками.
- [ ] Кнопки `Back` и `Main` возвращают к ожидаемым карточкам.
- [x] Успешные dialog submit закрывают модалку и показывают результат отдельной карточкой.
- [ ] Ошибки валидации dialog остаются внутри формы и подсвечивают конкретное поле.

## Accounts

- [ ] OpenAI account list показывает количество configured/authorized accounts.
- [ ] OpenAI account status открывается кнопкой без ввода account name.
- [ ] Если device-code истек, account можно снова авторизовать кнопкой.
- [ ] Delete OpenAI account работает кнопкой и удаляет Kubernetes auth secret.
- [ ] GitHub account list показывает несколько accounts с masked статусом, username/email/scopes.
- [ ] GitHub account add/edit/delete доступны через buttons/dialogs без ввода внутренних id.
- [ ] GitHub token validation подтягивает username/email через GitHub API.

## Repositories

- [ ] Repository onboarding начинается с выбора GitHub account кнопкой.
- [ ] Repository search возвращает приватную карточку с результатами.
- [ ] Branch selection выбирает branch из GitHub API.
- [x] Repository binding сохраняет provider/owner/name/default branch/account.
- [ ] Webhook создается или проверяется при onboarding.
- [ ] Repository card имеет check/webhook/edit/delete actions без ручного owner/name.

## Projects

- [x] Project создается из `/agents -> Projects -> Create project`.
- [x] Создание project создает или привязывает Mattermost team.
- [ ] Project dashboard показывает team, repositories, roles, chats и linked counters.
- [x] Repository можно привязать к project из dashboard через select.
- [ ] Project dashboard дает быстрые кнопки add repo, add role, create chat.

## Roles

- [x] Agent role создается из project dashboard.
- [x] Role editor позволяет выбрать project, role type, OpenAI account, GitHub account.
- [x] Prompt template можно оставить пустым.
- [x] Empty prompt template сохраняет `prompt_mode=raw` и считается валидным режимом.
- [ ] Advanced/Codex settings сохраняются в role.
- [x] Kubernetes access mode выбирается явно.
- [ ] Role card показывает account bindings и advanced settings summary без секретов.

## Chats

- [x] Chat создается из project dashboard.
- [x] Chat создается как private Mattermost channel внутри project team.
- [ ] Chat creator позволяет выбрать manager/pm/worker+reviewer/single/multi custom mode.
- [x] Chat creator позволяет выбрать роли без ввода role id.
- [x] Chat creator позволяет выбрать repository без ввода repository id.
- [ ] Chat card показывает roles, repositories, issue context и channel id.
- [x] Bot account состоит в созданном private channel.

## Chat-Triggered Runs

- [x] Сообщение owner в project chat распознается bot-service через Mattermost event listener.
- [x] Сообщение самого bot-service не запускает новый run.
- [x] Если chat неизвестен системе, run не запускается.
- [ ] Если role не настроена или OpenAI account отсутствует, owner получает понятную ошибку в thread.
- [x] Если role без prompt template, user message становится основной инструкцией.
- [x] Prompt context содержит project, chat, role, repositories, issue/work policy и locale.
- [x] Manager/PM/ad-hoc role запускается как chat run без обязательного PR.
- [x] Worker role с repository запускает developer run, создает branch и draft PR при изменениях.
- [ ] Reviewer role запускает review run, если в сообщении есть GitHub PR URL/number.
- [x] Start message появляется в thread исходного сообщения.
- [x] Final success/failure message появляется в thread исходного сообщения.
- [ ] PR URL, branch или review decision попадают в thread/card, если появились в artifacts.

## Runtime

- [ ] Runtime menu показывает active/completed/failed runs.
- [ ] Run card открывается кнопкой без ввода run id.
- [ ] Run card показывает Kubernetes job/pod/status/log tail/artifacts.
- [ ] Cleanup конкретного run запускается кнопкой с карточки.
- [ ] Retention dry-run показывает skipped active jobs и matched resources.
- [ ] Retention apply требует confirmation dialog.
- [ ] Held/waiting legacy flows не удаляются retention cleanup без явного owner action.

## Full Dogfooding Scenario

- [x] Создан test project для проверки.
- [x] В project привязан test repository.
- [x] Создана worker role с OpenAI и GitHub account.
- [x] Создан chat с worker role и test repository.
- [x] Owner пишет задачу в chat.
- [x] Agent pod стартует в Kubernetes namespace Mattermost runtime.
- [x] Agent pod получает отдельный PVC.
- [x] Agent pod получает только нужные Kubernetes secrets.
- [ ] Agent использует `gh` через выбранный GitHub account.
- [x] Agent использует Codex account через выбранный OpenAI auth secret.
- [x] Agent создает draft PR или сообщает no-changes.
- [ ] Thread содержит итоговый ответ агента и ссылку на GitHub entity.
- [x] Runtime cleanup удаляет job/PVC/configmap после проверки.
