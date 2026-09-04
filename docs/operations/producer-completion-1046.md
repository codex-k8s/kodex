---
id: OPS-ISSUE-1046
title: Доказательства corrective producer unit
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-05
---

# Промежуточная интеграция #1046

Это промежуточная передача контрактов, не заявление о завершении #1046.
База: `1cf399a5f04145395fa746a8f0e7527e5cad01ff`; scheduler и integration
сохранены. Policy revision: 46, включая новые scheduler operations revision 44.

## Стабильные контракты

- `BootstrapState.speech_transcription`: `eligible`, `available`, `reason`.
  CP возвращает `available=false`: runtime проверяет gateway через STT.
- `PlatformQueryService.ListManagedConfigurations`: `project_ref`, `kind`,
  `query`, `page`; ответ `configurations`, `total`, `page`. Content не выдаётся.
- `ListProjectMembershipsRequest.query=3`, прежние поля сохранены.
- `Membership.project_ref=9`: project membership сохраняет свой проект;
  platform membership возвращает пустое значение.
- `GetRuntimeEnvironmentDraft` и специализированные команды
  `CreateRuntimeEnvironmentDraft`, `SaveRuntimeEnvironmentDraft`,
  `ValidateRuntimeEnvironmentDraft`, `PublishRuntimeEnvironmentDraft`,
  `DiscardRuntimeEnvironmentDraft`. Все команды используют `MutationContext`.
  Draft содержит `ref`, `version`, `project_ref`, `environment_ref`,
  `expected_environment_version`, `state`, `specification`, `validation_digest`,
  `diagnostics`, `published_environment_ref`; publish дополнительно возвращает
  `environment`. Create допускает неполную спецификацию без публикации.
- `TranscriptionPolicyProjectionService.ResolveTranscriptionPolicy`: существующий
  контракт получил CP producer и регистрацию; locator сверяется с verified context.
- `RuntimeCredentialProjectionService.MaterializeSystemAssistantCredentials`:
  `execution: MaterializeRuntimeCredentialsRequest`; ответ `projection`.

## Границы

| Сценарий | Инициатор и authority | Owner и проверка | Результат |
|---|---|---|---|
| Каталоги Agents/Workflows/Runs/Schedules/Environments/Secrets/Members | Gateway, verified actor/tenant и optional project | CP, повторная application eligibility каждого результата в одном снимке | Ограниченная страница; cursor связан с actor/tenant/filter; событий нет |
| Managed catalog | Gateway, verified actor/tenant | CP, project.view или organization.view | Метаданные и безопасная current revision; точный eligible total; событий нет |
| STT policy | STT continuation от gateway | CP, exact locator и platform.stt.use root actor; enabled config, исполняемая model/language, account и API-key generation | Immutable config identity; не runtime readiness; событий нет |
| STT credential | STT continuation, secret-broker exact peer | CP повторно проверяет end-user eligibility и config/account generation | Exact credential descriptor, без выдачи credential клиенту |
| Project runtime credential | Runtime-controller, execution proof | CP exact lease/fence/generation/session/turn/revision | Project обязателен |
| Global assistant credential | Runtime-controller, отдельная execution operation | CP проверяет system_key, server-owned session/root/turn lineage и отсутствие project/secrets | Org projection; обычная operation не ослаблена |
| Environment draft create/save/validate/discard | Gateway, verified actor/tenant, project.manage | CP owner transaction, OCC и idempotency; scope существующего draft выводится из БД | DRAFT/VALID/INVALID/DISCARDED; audit и receipt, без события; GetRuntimeEnvironmentDraft является read path |
| Environment draft publish | Те же полномочия; VALID и exact dependency digest | CP атомарно публикует environment, проверяет target version и digest; закрывает draft | PUBLISHED; прежний environment AGENT_CHANGED event и idempotent readback |
| VFS и managed catalog | Gateway, verified actor/tenant и optional project | SQL eligibility до LIMIT; exact authorized total; cursor связан с областью | Только ограниченная страница; VFS path не источник полномочий |

## Проверки промежуточной передачи

- `make test-control-plane-postgres`: PASS локально. Повторный прогон включает
  organization-scoped STT credential и stale workspace assertion из #1028.
- `GOWORK=off go test ./...` в control-plane и controlplaneclient: PASS локально.
- Targeted Go secret-broker transport/kubernetes и runtime-controller
  credentialprojection: PASS локально.
- `make test-authority-policy-codegen check-proto-codegen`: PASS локально.
- Дополнительная матрица `TestRuntimeCredentialProjectionScopeMatrix` и
  `TestOrganizationProjectionScopeIsMethodSpecific` закрепляет различие scope.
- Workspace assertion сравнивает весь `runtimeWorkspacePolicy()`, включая
  read-only credential file из #1047; production policy не менялась.
- Повторный PostgreSQL suite с environment draft lifecycle и SQL/evaluator
  parity: PASS локально. Проверены incomplete/invalid, save OCC, validate,
  publish replay, terminal rejection, target environment version conflict,
  discard. Файл: `environment_draft_component_test.go`.
- `TestEnvironmentDraftPolicyRoundTrip`: PASS локально; DNS TCP/UDP сохраняет
  одну destination в редактируемой спецификации.
- `make lint-proto build-proto check-proto-codegen test-authority-policy-codegen`:
  PASS локально. Authority continuation получил отдельный response wrapper;
  номера и типы wire-полей сохранены, lint не отключён.
- Targeted authorityclient и authority transport: PASS локально.
- `make test-web-only-release`: PASS локально с policy revision 46.

## Ещё не закрыто

Настоящий SkillBundle и KodexMemoryRecord; полный VFS дерева сущностей;
revision impact и selected rebind secrets/environments; проверка Git lifecycle;
полные STT model params и immutable projection; mail authorization producer #1037;
полная негативная матрица, race и финальный exact-SHA review. Обычные каталоги
используют bounded candidate batches и per-result eligibility в Go; VFS и managed
catalog уже выполняют eligibility до SQL LIMIT. Это не завершённый unit.

Live, deploy, push, PR, merge: NOT RUN, запрещены текущим заданием.
Значения секретов не публиковались. HTTP и PWA меняют отдельные владельцы.
