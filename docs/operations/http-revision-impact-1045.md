---
id: OPS-HTTP-REVISION-IMPACT-1045
title: HTTP публикация Environment и инструкций с планом замен
type: operations
status: approved
owner: developer
version: 1.1.0
updated: 2026-09-06
---

Источник: #1045/#1046, MVP-UI-47, owner644/policy69 в CP89. Полная
owner-матрица находится в `revision-impact-plan-1046.md`.

Дополнение owner70 из CP `c1a7fb8cdb02214f4b0187bd879a60b91c6a43a7`:

| Инициатор и authority | HTTP → RPC | Переход и чтение |
| --- | --- | --- |
| Verified actor; owner разрешает Agent и проверяет действующее право изменения инструкций | POST agents/{agentRef}/instructions/impact-plans → PrepareInstructionsImpact | If-Match Agent, idempotency, CSRF; 200 PREPARED, audit без события |
| Verified actor; owner разрешает configuration и source scope | POST prompt-template-configurations/{configurationRef}/revisions/{revisionRef}/impact-plans → PreparePromptTemplateImpact | If-Match configuration, exact revision, idempotency, CSRF; 200 PREPARED, audit без события |
| Явный выбор item refs из плана | POST agents/{agentRef}/instruction-commands action=PUBLISH → PublishInstructionDraft | Обязательные planRef/selectedItemRefs, atomic INSTRUCTIONS_PUBLISHED; ответ agent ref/projectRef/version + APPLIED plan |
| Явный выбор item refs из плана | POST prompt-template-configurations/{configurationRef}/revisions/{revisionRef}/publication → PublishPromptTemplateDraft | Обязательные planRef/selectedItemRefs, atomic MANAGED_CONFIGURATION_CHANGED; configuration/revision + APPLIED plan |
| Неизвестный ответ либо новый просмотр | GET Agent / managed configuration и общий GET revision-impact-plans/{planRef} | Owner receipts и текущий защищённый readback; повторный effect не создаётся |

Два Prepare проходят canonical signed UNARY policy70 с точными resource/OCC
и digest protobuf. Payload не назначает actor или authority. Пустой список
selectedItemRefs допустим, отсутствие списка, дубликаты и более 1000 refs
отклоняются до owner. Для VALIDATE/ROLLBACK поля плана не принимаются.
Instructions publication receipt содержит минимальные метаданные Agent;
полный Agent и фактическая instructionBinding читаются отдельно. Значение
effective=false сохраняется явно: исторический publishedInstructions не
подменяет выбранную binding и не доказывает её эффективность.

Mapper допускает пустой sourceRevisionRef только для первой Prompt публикации.
Для организационного Agent/AGENT_CONTINUATION в Prompt plan projectRef пустой;
все остальные items требуют projectRef. APPLIED Prompt меняет bindingVersion,
но consumerVersion может остаться прежней. Environment и Instructions требуют
роста consumerVersion. Все APPLIED results связаны с опубликованной revision
и прежней binding; неизвестные kind/outcome и противоречивые pins дают 502.

| Инициатор и authority | HTTP → RPC | Переход и чтение |
| --- | --- | --- |
| Подписанный actor, текущая project.manage владельца draft | POST runtime-environment-drafts/{draftRef}/impact-plans → PrepareEnvironmentDraftImpact | If-Match VALID draft, idempotency, CSRF; 201 immutable plan без публикации |
| Тот же actor и текущая source authority | GET revision-impact-plans/{planRef} → GetRevisionImpactPlan | Query/page передаются владельцу, total после его текущей eligibility; без события |
| Явная публикация и выбор server item refs | POST draft/publication → PublishRuntimeEnvironmentDraft | Обязательный planRef и явный selectedItemRefs (0..1000), If-Match/idempotency/CSRF; atomic publication и outcomes |
| Неизвестный ответ | GET draft и GET plan | Exact published Environment revision и APPLIED plan; новый effect для восстановления не создаётся |

Старый запрос публикации без плана отклоняется до RPC, включая попытку
воспроизвести старый receipt. Его неизвестный результат восстанавливается
через прежний защищённый GetDraft. Для новых попыток клиент сохраняет
draftRef/planRef и idempotency key, но не назначает actor или authority.

Ответ публикации содержит draft, environment и plan из одной command receipt.
Gateway не заменяет его latest GET. Проверяются draftVersion+1, исходный OCC,
plan APPLIED/version2, exact target digest и опубликованная Environment
revision. Новый Environment имеет пустой source tuple; существующий — полный
immutable base tuple. SourceVersion не подменяется DraftVersion.

Get проверяет закрытые состояния, safe53 версии, refs, digest, timestamps,
ограничение1000, уникальные item refs и current total не выше plan.total.
APPLIED item сохраняет binding ref и увеличивает обе версии, ссылаясь на
опубликованную target revision. Остальные outcomes не содержат result pins.
Выбор пустого массива публикует без замен; CONFLICT/FORBIDDEN отдельного item
не отменяет уже подтверждённую публикацию.

Текущий executable registry этого checkpoint обслуживает Environment.
Общие Proto kinds Prompt/Instructions не объявляются реализованными до их
отдельного owner checkpoint. Values Environment и Secret не выдаются в impact
plan. Реальная публикация сохраняет owner events; plan/expiry читаются без
нового события через Get и durable audit/receipt.

HTTP проверки закрепляют prepare/read/publish mapping, пустой selection,
отказ старого wire, duplicate selection, неизвестные enum и подмену result
binding. Локальный exact ledger публикуется с checkpoint PR1066. Browser,
integrated protected route и deployment проверяются отдельно; fixture HTTP
не подменяет эти проверки. Секреты не раскрыты.
