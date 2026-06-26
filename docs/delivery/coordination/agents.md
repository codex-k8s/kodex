# Архив раскладки шести доменных агентов

## Назначение

Этот файл является архивным индексом прежней фазы параллельной доменной разработки шестью агентами. Он оставлен для трассировки уже выполненных срезов и понимания исторического ownership.

Активная модель нового окружения описана в `roles.md`. Новые назначения нужно формулировать через роли и GitHub Issues, а не через номера `#1` ... `#6`.

В этом архиве фиксируются только:

- номер агента;
- основной домен;
- основные сервисы;
- ссылка на личный файл координации агента;
- стабильная зона ответственности.

Текущий прогресс, временные переключения, блокировки, открытый бэклог и рекомендации следующего шага больше не размещаются в этом файле. Старые личные файлы агентов остаются архивом предыдущей модели.

## Карта ответственности

| Агент | Личный файл | Основной домен | Основные сервисы | Стабильная ответственность |
|---|---|---|---|---|
| #1 | `agent-1-project-catalog.md` | Проекты, репозитории, runtime, fleet и platform MCP | `project-catalog`, `runtime-manager`, `fleet-manager`, `platform-mcp-server` | Проекты, репозитории, проверенная версия `services.yaml`, источники документации, правила веток, релизные политики, слоты, workspace, platform jobs, cleanup, prewarm, reuse, серверы, Kubernetes-кластеры, health, placement scope и MCP-поверхность без бизнес-состояния. |
| #2 | `agent-2-provider-hub.md` | Provider-native интеграции и внешний HTTP-вход интеграций | `provider-hub`, `integration-gateway` | Внешние Git-провайдеры, репозитории, Issue, PR/MR, комментарии, review-сигналы, webhook, локальные проекции, сверка внешнего состояния, лимиты и операции провайдера на границе ссылок на секреты; `integration-gateway` как тонкий HTTP-вход webhook/callback событий без бизнес-состояния. |
| #3 | `agent-3-package-hub.md` | Пакетная платформа | `package-hub` | Источники пакетов, доступный каталог, версии, manifest, установки, схемы секретов, верификация пакетов и события `package.*`. |
| #4 | `agent-4-interaction-hub.md` | Центр взаимодействий | `interaction-hub` | Диалоги, запросы обратной связи, доставка Human gate и approval request, уведомления, подписки, delivery attempts, callback внешних каналов и стабильный channel delivery/callback contract поверх package-owned runtime; decision state остаётся у сервиса-владельца решения. |
| #5 | `agent-5-codex-hook-ingress.md` | Входной контур Codex hooks | `codex-hook-ingress` | Нормализованные Codex hook events, hook envelope schemas, sanitizer contract, hook emitter/local sidecar runtime contract, safe previews/digests/refs, source binding, routing safe event parts владельцам и отделение hooks от MCP tools и business commands. |
| #6 | `agent-6-risk-governance.md` | Риски и релизы | `governance-manager` | Risk profiles, risk rules, risk assessments, review signals, gate policy, Human gate decisions, release decision packages, release decisions, release safety-loop и события `governance.*`; delivery и callbacks остаются у `interaction-hub`, project/release policy остаётся у `project-catalog`. |

## Как читать координацию

- Для новых задач сначала открыть `roles.md`.
- Этот индекс использовать только когда нужно понять, какой прежний доменный агент вёл исторический срез.
- Если новая задача продолжает старый срез, в Issue указывать домен и роль, а не номер прежнего агента.
- Если меняется активная ролевая модель, обновлять `roles.md`, а не этот архив.

## Правила обновления

- Не добавлять сюда статусы вида `готово`, `на ревью`, `в PR`, списки закрытых PR, временные переключения и детальные блокировки.
- Не дублировать бэклог личных файлов агентов.
- Не использовать этот файл как источник новых назначений в ролевом окружении.
- Междоменные решения фиксировать в доменной документации, `roles.md` или GitHub Issues; этот архив обновлять только для исправления исторической неточности.
