---
id: OPS-CONTEXT-HTTP-1045
title: HTTP передача Skills и Kodex Memory
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-05
---

# Контрактная передача HTTP

Источник: Issue #1045, #1046, MVP-UI-37 и
`docs/operations/mvp-1031-acceptance.md` из QA worktree #1031.
Потреблён точный CP checkpoint
`d977531546d43f681c94da5aa2a8d32a7ba64562`, policy revision 51.
Семантика владельца зафиксирована в `context-contracts-1046.md`.

Дополнительно потреблён CP `7df60ddef88348aaf47dd50d0d22737d1e83fec3`:
MemoryRecord create/revise/archive/restore/purge, get/list/history имеют
SQL/domain implementation. Из `8f8e88aa8` и `bd674280e` подключены Skill
lifecycle и agent bind/unbind обоих видов. Runtime materialization и реальный
scanner deploy ещё реализует #1046. HTTP не заменяет отсутствующий owner
lifecycle локальным state и не превращает ошибку CP в успешный результат.
Полный unit #1045 не финализирован.

## Paths И SDK

| HTTP | Generated SDK | CP RPC |
|---|---|---|
| `GET /api/v1/skill-bundles` | `listSkillBundles` | `ListSkillBundles` |
| `GET /api/v1/skill-bundles/{bundleRef}` | `getSkillBundle` | `GetSkillBundle` |
| `GET /api/v1/skill-bundles/{bundleRef}/revisions` | `listSkillBundleRevisions` | `ListSkillBundleRevisions` |
| `POST /api/v1/skill-bundles/{bundleRef}/archive` | `archiveSkillBundle` | `ArchiveSkillBundle` |
| `POST /api/v1/skill-bundles/{bundleRef}/restoration` | `restoreSkillBundle` | `RestoreSkillBundle` |
| `POST /api/v1/skill-bundles/{bundleRef}/purge` | `purgeSkillBundle` | `PurgeSkillBundle` |
| `PUT /api/v1/agents/{agentRef}/skill-bundles/{bundleRef}` | `bindAgentSkillBundle` | `BindAgentSkillBundle` |
| `DELETE /api/v1/agents/{agentRef}/skill-bundles/{bundleRef}` | `unbindAgentSkillBundle` | `UnbindAgentSkillBundle` |
| `GET /api/v1/memory-records` | `listMemoryRecords` | `ListMemoryRecords` |
| `GET /api/v1/memory-records/{recordRef}` | `getMemoryRecord` | `GetMemoryRecord` |
| `GET /api/v1/memory-records/{recordRef}/revisions` | `listMemoryRecordRevisions` | `ListMemoryRecordRevisions` |
| `POST /api/v1/memory-records/{recordRef}/archive` | `archiveMemoryRecord` | `ArchiveMemoryRecord` |
| `POST /api/v1/memory-records/{recordRef}/restoration` | `restoreMemoryRecord` | `RestoreMemoryRecord` |
| `POST /api/v1/memory-records/{recordRef}/purge` | `purgeMemoryRecord` | `PurgeMemoryRecord` |
| `PUT /api/v1/agents/{agentRef}/memory-records/{recordRef}` | `bindAgentMemoryRecord` | `BindAgentMemoryRecord` |
| `DELETE /api/v1/agents/{agentRef}/memory-records/{recordRef}` | `unbindAgentMemoryRecord` | `UnbindAgentMemoryRecord` |
| `POST /api/v1/projects/{projectRef}/skill-bundle-drafts` | `createSkillBundleDraft` | `CreateSkillBundleDraft` |
| `PUT /api/v1/skill-bundles/{bundleRef}/revisions/{revisionRef}` | `saveSkillBundleDraft` | `SaveSkillBundleDraft` |
| `POST /api/v1/skill-bundles/{bundleRef}/revisions/{revisionRef}/validation` | `validateSkillBundleDraft` | `ValidateSkillBundleDraft` |
| `POST /api/v1/skill-bundles/{bundleRef}/revisions/{revisionRef}/review` | `reviewSkillBundleDraft` | `ReviewSkillBundleDraft` |
| `POST /api/v1/skill-bundles/{bundleRef}/revisions/{revisionRef}/publication` | `publishSkillBundleDraft` | `PublishSkillBundleDraft` |
| `POST /api/v1/skill-bundles/{bundleRef}/revisions/{revisionRef}/discard` | `discardSkillBundleDraft` | `DiscardSkillBundleDraft` |
| `POST /api/v1/projects/{projectRef}/memory-records` | `createMemoryRecord` | `CreateMemoryRecord` |
| `POST /api/v1/memory-records/{recordRef}/revisions` | `reviseMemoryRecord` | `ReviseMemoryRecord` |

## Authority И Lifecycle

- Browser session/OIDC, organization, CSRF/Origin и bounded deadline остаются
  общими boundary. Create требует проверенный project context из path.
  Optional projectRef/agentRef в list — только фильтры, не полномочия.
  Остальные операции разрешают owner по opaque resource ref в CP.
- List по умолчанию ACTIVE, поддерживает ACTIVE/ARCHIVED/EXPIRED/PURGED.
  Pagination: pageSize 1..100, default 50, cursor <=512. Ответ items/total/
  nextPageToken сохраняет CP cursor и project grouping без client fan-out.
- Create Skill без bundleRef — новый aggregate, без If-Match. Create с
  bundleRef — следующая draft, If-Match aggregate version обязателен.
  Save/validate/review/publish/discard и archive/restore/purge используют
  If-Match aggregate version + idempotency key.
- Validate/review/publish/discard закрепляют expectedDigest и revisionRef.
  Review принимает APPROVE/REJECT и comment, не actor или scan result.
  Publish и runtime activation не синонимы: материализацию выполняет CP/runtime.
- Memory create принимает optional agentRef и specification. RetentionUntil
  обязателен, sourceRunRef — optional owner-resolved provenance locator.
  Actor/sourceKind/digest/review/scan fields не принимаются от браузера.
  Revise создаёт immutable revision с If-Match record version.
- Agent bind/unbind: If-Match = **agent version**, body revisionRef +
  expectedBindingVersion (0 означает отсутствие binding). Response содержит
  точную binding version, **не ETag agent**; после мутации нужен авторитетный
  GetAgentRuntimeConfiguration перед следующей agent mutation. Ответ содержит
  обязательные массивы skillBindings/memoryBindings и agentVersion из одной
  owner transaction; пустые bindings возвращаются как `[]`. HTTP не выдумывает
  новую agent version. Все семь операций, возвращающих runtime configuration,
  проверяют соответствие binding агенту, refs/digest/version, отсутствие дублей
  и общий предел 128. Ошибка не выдаёт частичный snapshot или новый ETag.
- Archive допускает restore. Purge необратим и оставляет metadata/tombstone.
  Memory EXPIRED/PURGED требует redacted revision с пустым summary; неполная
  redaction закрыто отклоняется HTTP. History использует тот же typed view.
  Изменение/архивация/rebind не меняют уже запущенный attempt.
- Все изменения, idempotency receipts, audit/events и revoke старых bindings
  принадлежат транзакции CP. HTTP не создаёт события. Event origin/cardinality
  и runtime consumer acceptance подтверждаются отдельным owner checkpoint.
  Query не имеет эффекта; tombstone/expired metadata читаются авторитетно.
- В CP `7df60ddef` Memory create/revise/archive/restore/purge не создают
  domain event: авторитетный read path — GetMemoryRecord, ListMemoryRecords
  и ListMemoryRecordRevisions. Archive/purge выключают bindings в owner
  transaction, restore не включает их обратно. Runtime consumer ещё не готов;
  проверка ACTIVE/retention перед каждой attempt остаётся его обязательством.

## Границы Формата

- Skill specification: name <=160 и description <=2000 Unicode символов,
  files 1..128. Каждый файл имеет относительный canonical manifest path
  <=240 UTF-8 bytes, artifactRef и положительную exact artifactRevision.
  Traversal, абсолютные пути, dotfiles, whitespace по краям сегментов,
  Windows separators, регистронезависимые дубли и небезопасные JSON integers
  отклоняются до CP. Обязателен root `SKILL.md` с точным регистром;
  supporting files только md/txt/json/csv/png/jpg/jpeg/webp. HTTP не читает
  filesystem по path и не считает эти проверки malware scan.
- Ownership artifact revision и содержимое manifest/scan/policy проверяет CP.
  Draft уже должен иметь допустимый набор paths, но его содержание может ещё
  не пройти validation. Scan/review/provenance доступны только в ответе.
- File digest сохраняет точный artifact receipt `sha256:<64 hex>`, в отличие
  от aggregate/provenance/scan digest без префикса. HTTP не переписывает digest.
  Файл <=32 MiB, сумма <=64 MiB; CP дополнительно ограничивает `SKILL.md`
  256 KiB при validation. Purged revision может иметь пустой files.
- Memory title <=160 Unicode символов, summary <=65536 UTF-8 bytes;
  оба непустые при create/revise, NUL запрещён.
  RetentionUntil — корректный timestamp. Разрешённый срок и source run
  eligibility проверяет CP. Diagnostics <=128 строк по 2000 bytes.
- Все integer versions <=9007199254740991. Неизвестные enums, corrupt
  provenance/timestamps/digests, ref mismatch, чужой project в filtered list,
  неполная redaction и перепутанная binding закрыто дают 502.
- Эти HTTP format bounds нужно сохранить согласованными с owner implementation
  при следующем CP checkpoint; canonical source остаётся Proto/owner contract.
  Никакое состояние scan/review/runtime readiness не ослаблено для fixture PASS.

## Проверки

Локально выполнены:
`go test -race ./internal/transport/http ./internal/app ./internal/security/boundary -count=1`,
focused `go vet`, OpenAPI Go/TS generation и strict generated SDK typecheck.
`TestContextResourceEveryTypedRoute` покрывает все 24 mappings, OCC,
idempotency, cursor, refs и отсутствие ложного agent ETag.
Дополнительные tests проверяют malformed manifest, подмену authority/scan,
unsafe versions, retention field, Unimplemented/401/403/404/412/503,
corrupt response и redaction. Общий PWA прогон не повторялся.
`TestMemoryOwnerProjectionHTTP` отдельно проверяет формы CP `7df60ddef`
на get/list/history: USER_SUMMARY с source run и без него, все четыре states,
redacted историю при активной записи, сохранение retention/digest/provenance.
Это проверка HTTP с fake gRPC response по сверенной SQL projection,
не запуск PostgreSQL и не сквозной protected CP integration.

После подключения `bd674280e` выполнены полный `go test -race ./... -count=1`,
`go vet ./...` и `go build ./...` в control-api-gateway: локальный PASS.
Новые `TestRuntimeContextBindings*` покрывают все семь runtime responses,
пустые массивы, точный agent ETag и закрытый отказ на повреждённых bindings.
`TestSkillOwner*` проверяют Unicode/UTF-8 limits, paths, file size limits и
неизменённый artifact digest. Обнаруженные четыре отсутствующих CP message IDs
добавлены в оба HTTP locale catalogs; полный catalog test проходит.
Go/TypeScript codegen и отдельный strict typecheck generated SDK: PASS.
Общий PWA typecheck в HTTP worktree остаётся FAIL: незавершённые Schedule/
IntegrationDefinition fixtures и новый обязательный binding readback в runtime
fixture. Handwritten PWA меняет исполнитель #1022, schema не ослаблена.

Через Context7 проверена [документация oapi-codegen](https://pkg.go.dev/github.com/oapi-codegen/oapi-codegen/v2):
generated models по referenced schemas не заменяют request/response validation.
Новые bounds проверяются исполняемым HTTP adapter и тестами, а не только YAML.

NOT RUN: новые CP owner SQL и runtime materialization в HTTP integration,
браузерный пользовательский lifecycle, live providers, staging/deploy.
Наличие generated API не закрывает эти acceptance criteria.

## Координация

STT typed HTTP checkpoint:
`1ad6c96e263abf680b5bca1bb5a666a39b2b05e7`.
GET system-stt-configuration возвращает полные parameters/limits.
POST system-stt-configurations/typed-drafts сериализует specification
в immutable managed JSON; исходный raw editor и validate/publish сохранены.

В этом треде нет инструмента отправки сообщения в тред Bohr. Контракты
потреблены из его зафиксированной передачи, а не согласованы посредством
несуществующего обмена сообщениями. Этот документ — явная обратная передача
HTTP границ для сверки CP/PWA через root.
