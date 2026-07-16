# Инструкции проекта matter-codex

## Общие правила

- Ответы пользователю, статусы, PR-описания и документацию писать по-русски.
- Commit message писать на английском языке.
- Перед началом работы читать этот файл и проверять текущую ветку.
- Секреты из `.env`, kubeconfig, GitHub, OpenAI и Mattermost не выводить в сообщения, логи, PR и prompt.
- Значения env не документировать. Допустимо документировать только имена ключей и факт, что значение задано.
- Для актуальной информации по библиотекам использовать Context7 MCP.
- Перед изменением архитектуры читать `docs/architecture/**`, `docs/domains/**` и релевантные ADR из `docs/decisions/**`.
- Для реализации использовать `docs/guides/**`, `docs/design-guidelines/AGENTS.md`, `docs/design-guidelines/common/check_list.md` и профильный checklist.
- Для OpenAI и Codex использовать официальные OpenAI docs или Codex manual.
- Для GitHub-операций использовать соответствующие GitHub skills.

## Git и PR

- Работу вести в отдельных ветках.
- `main` использовать только как базовую ветку.
- Каждый PR должен быть вручную тестируемым владельцем.
- В PR явно писать, что можно проверить вручную и какие автоматические проверки запускались.
- Если в процессе найдено противоречие между документацией, окружением или запросом владельца, остановиться и предложить варианты.

## Kubernetes и окружение

- Целевой runtime MVP работает в Kubernetes.
- Для bootstrap/deploy скриптов читать `.env`, но печатать только безопасные статусы проверок.
- Agent pod получает только необходимые секреты и только через Kubernetes Secret/env/file mount.
- У каждого agent run должен быть собственный рабочий каталог и PVC.
- Для MVP выбран один Mattermost runtime namespace: Mattermost, bot-service, agent pod и PVC живут в нем. Имя берется из `MATTERCODEX_NAMESPACE` или `PRODUCTION_NAMESPACE`, если владелец не попросит пересмотреть решение.

## Документация

- Активные продуктовые решения хранить в `docs/product/**`.
- Активную системную архитектуру и границы данных хранить в `docs/architecture/**` и `docs/domains/**`.
- Существенные решения оформлять ADR в `docs/decisions/**`.
- Правила реализации хранить в `docs/guides/**` и детальных `docs/design-guidelines/**`.
- Production contract хранить в `docs/operations/**`, а пошаговые действия - в `docs/runbooks/**`.
- Волны, result gates и dogfooding process хранить в `docs/roadmap/**`.
- `docs/strategy/**` является superseded MVP baseline и не используется как источник новых решений.
- Исходные документы идеи находятся в `docs/idea/**`.
- Документы должны описывать целевое состояние и явно помечать принятые и открытые решения.

## Human gates и агентная работа

- Human gate относится к законченному типу результата, а не к каждому внутреннему шагу.
- По умолчанию result проходит 2-3 reviewer/fix цикла до первого owner gate.
- После owner feedback worker исправляет результат, reviewer выполняет полный повторный проход, owner дает final OK.
- Merge выполняет reviewer только после final owner OK.
- После merge reviewer запускает improver через MCP для обновления instructions/guides/playbooks по замечаниям цикла.
- Агенты запускают других агентов только через MatterCodex MCP tools; текстовые mentions от bot identities не являются execution trigger.
