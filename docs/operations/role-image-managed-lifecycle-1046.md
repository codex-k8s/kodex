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

Git write-back остаётся отдельным обязательным owner lifecycle с Human Gate,
exact base commit/path и effect receipt; read-only SourceWork его не заменяет.
Эта матрица задаёт оставшийся scope. До executable checkpoint UI publication,
specialized Manage lineage и actual selective rebind — NOT RUN.

Public additive contract: ListRoleImageRecipes получает literal `query` и
`state` (пусто либо ACTIVE/ARCHIVED), response `total` считает все видимые
совпадения независимо от cursor. Cursor связан с actor/tenant/project/role/query/state.
`RoleImageRecipe.managed_lineage` содержит configuration ref, immutable
revision ref/number, managedBy UI/GIT, source ref/revision и origin
BASELINE/MANAGED. `ImageBuild.configuration_revision_ref` связывает конкретную
попытку с тем же input; старый build без доказанного mapping оставляет поле
пустым. Content/Dockerfile этим дополнением не раскрываются. RPC и policy
операции прежние; новые поля source ещё требуют executable owner readback.
