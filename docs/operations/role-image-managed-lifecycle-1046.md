---
id: OPS-DOC-1046-ROLE-IMAGE-MANAGED
title: Публикация RoleImage из UI и Git в реальную сборку
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-05
---

# Матрица владельца RoleImage

Источник: #1046, Epic #1018, CFG и MVP-UI-42/47. Владелец recipe, managed
revision, build и admission — control-plane. Существующие generated RPC
ManageRoleImageRecipe и Create/Save/Validate/PublishRoleImageRevision не
создают параллельных исполняемых объектов. Image-builder/admission/publisher
потребляют прежний защищённый work protocol; новый deployable не добавляется.

| Сценарий | Actor и authority | Owner переход, OCC и receipt | Effect/read |
| --- | --- | --- | --- |
| UI draft create/save | Exact project image.build, owner до OCC | UI set и immutable DRAFT; server ref/parent | Existing protected managed history, без build |
| UI validate | Exact set/revision и catalog | Typed role/environment selection и текущий catalog; VALID/INVALID | Safe diagnostics, build отсутствует |
| UI publish | Exact UI set/version, VALID revision, повторная catalog/role authority | PUBLISHED и actual recipe generation/build/mapping635 одной TX; same intent replay | ROLE_IMAGE_RECIPE_CHANGED, existing recipe/build read |
| Specialized Manage CREATE/UPDATE | Existing project image.build и recipe OCC | UI managed revision и actual recipe/build одной TX; GIT content mutation запрещена | Точная связь configuration/revision/recipe generation/build |
| Git accept | SourceWork exact root actor/work/connection/package/commit/content | Та же typed publication, immutable Git provenance | Existing source receipt/read и actual build event |
| Detach/copy | Existing specialized owner command | Detach сохраняет историю, COPY получает новый server set; build только после publication | Protected managed read, no fabricated push |
| Build claim/renew/complete | Existing exact worker grant/fence/attempt/input | Existing build lifecycle, mapping указывает ровно published input | Existing worker readiness и typed build receipt |
| Admission/promotion | Existing provenance/SBOM/vulnerability/signature policy | Artifact selectable только после exact promotion readback | Existing immutable artifact, не произвольный image string |
| Selected consumer rebind | Exact owner/consumer versions и immutable impact | Только promoted artifact соответствующей managed revision | Actual environment revision/binding, per-item receipt |
| Archive/restore/retry | Existing specialized owner action и exact version | UI/GIT ownership проверяется до state change; старые grants закрываются owner TX | Existing authoritative recipe/build history |

List/Get/search используют одну видимость и возвращают server query/state
filter, total/cursor и безопасную связь source/revision/build. Source content
читается только через соответствующий protected read. Publication не означает
успешную сборку или promotion. Исторический BASELINE mapping635 содержит
фактический snapshot; неизвестные старые pins не изобретаются.

Git write-back использует отдельный owner lifecycle с Human Gate, exact base
commit/path и двумя effect receipts: proposal branch и PR/MR. Source branch
не изменяется; runtime получает новую revision только после merge и обычного
SourceWork sync. Исполняемый consumer #1028 зафиксирован в
`b889673f04bd431788ac7553e1a80b033852e431`; реальные GitHub/GitLab и deployed
acceptance остаются NOT RUN.

Actual selective promoted-artifact rebind реализован через migration643,
Prepare/GetRoleImageImpactPlan и специализированный RebindRoleImageConsumers.
Точная матрица, per-item outcomes и выполненные проверки приведены в
`role-image-impact-plan-1046.md`. Metadata binding фиксируется вместе с
реальным Environment effect и не заменяет переключение исполняемого образа.

Public additive contract: ListRoleImageRecipes получает literal `query` и
`state` (пусто либо ACTIVE/ARCHIVED), response `total` считает все видимые
совпадения независимо от cursor. Cursor связан с actor/tenant/project/role/query/state.
`RoleImageRecipe.managed_lineage` содержит configuration ref, immutable
revision ref/number, managedBy UI/GIT/SHIPPED, source ref/revision и origin
BASELINE/MANAGED. `ImageBuild.configuration_revision_ref` связывает конкретную
попытку с тем же input; старый build без доказанного mapping оставляет поле
пустым. Content/Dockerfile этим дополнением не раскрываются. RPC и policy
операции прежние. Owner readback связей, фильтров и счётчика подключён.

SHIPPED system-base назначается по server-owned input source
`platform-owned:default-role-image` и environment `system-base`. Он содержит
фактический release digest. Если mapping635 исторически отсутствует,
configuration/revision tuple остаётся пустым/0; новый managed UI объект не
выдумывается. Старый mapping сохраняется как история. UPDATE/ARCHIVE/RESTORE
и RequestBuild системной prebuilt базы отклоняются; nextActions не обещают
Git build для release-owned source. Изменение выполняется новой catalog recipe.

UI Validate проверяет actual environment catalog, Publish создаёт recipe/build
и mapping635 той же транзакцией. Specialized Manage CREATE/UPDATE сохраняет
каноническую опубликованную managed revision из server-resolved recipe input.
Если существует параллельный managed draft, direct UPDATE закрывается Conflict:
recipe OCC не заменяет version этого draft. GIT mutation также отклоняется.
SHIPPED generic/source commands закрыто отклоняются по actual owner provenance.

На дереве поверх `bdf7a0dc75a756613b38c71b730efe56d264bffb` полный
`TestBootstrapComponent` — PASS (21.238 s). Проверены actual managed lineage,
canonical content, build generation, parallel draft OCC, SHIPPED read-only,
literal query/state/total, actor/filter cursor, точная видимость и promotion.
Repository/transport/role-image domain race, полный control-plane vet/build,
SQL boundary, Proto replay, policy63 и authority ABI render — PASS.
Первоначальные isolated test fixture ошибки (raw principal вместо resolved,
чтение CurrentRevision вместо revisions страницы history) исправлены в тесте;
повтор targeted PostgreSQL — PASS (0.724 s). Browser/live — NOT RUN.
