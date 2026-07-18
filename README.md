# MatterCodex

Платформа для создания и управления ИИ-сотрудниками, которые работают в Mattermost, используют подключенные инструменты и выполняют управляемые человеком процессы. Платформа подходит для разработки, аналитики, документооборота, продаж, поддержки и других предметных областей.

## Статус

Действующий Mattermost-first runtime используется для dogfooding. Целевая production-архитектура, универсальная продуктовая модель и последовательность эволюции зафиксированы в активной документации. Исходная идея и superseded MVP strategy сохранены только как исторический контекст.

## Документация

- [Навигация и источники истины](docs/README.md)
- [Продуктовая модель](docs/product/README.md)
- [Архитектура](docs/architecture/README.md)
- [Домены](docs/domains/README.md)
- [Архитектурные решения](docs/decisions/README.md)
- [Инженерные гайды](docs/guides/README.md)
- [Production operations](docs/operations/README.md)
- [Эпики и волны](docs/roadmap/epics-and-waves.md)
- [Human gates по типам результата](docs/roadmap/result-human-gates.md)
- [Dogfooding bootstrap](docs/roadmap/dogfooding-bootstrap.md)
- [Mattermost bootstrap runbook](docs/runbooks/mattermost-bootstrap.md)
- [Bot-service runbook](docs/runbooks/bot-service.md)

## Mattermost Bootstrap

Kubernetes-операции выполняются на целевом сервере через SSH по `.env`.

```bash
bash scripts/env/check-env.sh --env-file .env
bash scripts/remote/k8s-preflight.sh --env-file .env
bash scripts/remote/install-foundation.sh --env-file .env --dry-run=server
bash scripts/remote/install-mattermost.sh --env-file .env --dry-run=server
```

### Изменение схемы Mattermost

Matter-codex осознанно меняет PostgreSQL-схему Mattermost во время установки. При `scripts/remote/install-mattermost.sh --apply` после старта Mattermost применяется миграция `deploy/k8s/mattermost/migrations/000001_post_message_max_length.sql`.

PostgreSQL image по умолчанию закреплен digest. Установщик блокирует его неявную смену для существующего PVC: изменение libc или правил сортировки без перестроения индексов способно нарушить уникальность данных. Контролируемое переключение выполняется только по `docs/runbooks/postgres-image-change.md`.

Встроенный PostgreSQL использует `pgvector/pgvector:0.8.5-pg16`, закрепленный неизменяемым digest из каталога внешних зависимостей: расширение `vector` необходимо для локальной перестраиваемой проекции памяти MatterCodex. Канонический текст памяти остается в обычных таблицах PostgreSQL, а при недоступности локального embedding runtime поиск продолжает работать через полнотекстовый индекс. Внешний embedding API не используется.

При подключении внешнего PostgreSQL администратор обязан заранее установить расширение `pgvector` совместимой версии; миграция MatterCodex выполняет `CREATE EXTENSION vector`, но не устанавливает системный пакет на сервер базы данных.

Миграция меняет колонку `posts.message` на `varchar(200000)`. Mattermost вычисляет runtime-лимит текста поста из размера этой колонки и делит его на 4 для худшего случая UTF-8, поэтому фактический лимит становится 50000 символов. Это нужно, чтобы длинные финальные ответы агентов не терялись.

Bot-service при этом все равно режет исходящие thread-сообщения по фактическому лимиту, прочитанному из `information_schema`, поэтому стандартный Mattermost с дефолтным лимитом тоже поддерживается.

Это осознанное изменение схемы стороннего приложения. Перед подготовкой matter-codex к opensource и перед обновлением Mattermost это решение надо отдельно пересмотреть.

Также `scripts/remote/install-mattermost.sh --apply` проверяет и включает `ServiceSettings.EnableUserAccessTokens=true`. Это нужно для role bot identities: агентские ответы публикуются от отдельных Mattermost users/bots через user access tokens. Если настройка выключена, Mattermost отклоняет эти token-ы, и bot-service вынужденно публикует сообщения от сервисного `matter-codex`.

## Bot-Service

Health-only deploy без Mattermost token:

```bash
bash scripts/remote/install-bot-service.sh --env-file .env --apply --wait
bash scripts/remote/smoke-bot-service.sh --env-file .env --check-url
```

Полный Mattermost bootstrap без ручного вывода token:

```bash
bash scripts/remote/bootstrap-mattermost-bot.sh --env-file .env
```
