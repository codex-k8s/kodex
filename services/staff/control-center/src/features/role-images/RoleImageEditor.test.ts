import { renderToString } from "@vue/server-renderer";
import { createSSRApp, defineComponent, h } from "vue";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type {
  RoleImageArtifact,
  RoleImageAdmissionFailure,
  RoleImageBuild,
  RoleImageRecipe,
} from "@/shared/api/generated/openapi/types.gen";
import { unavailableInventoryFixture } from "@/test-utils/image-inventory-fixture";
import { imageAdmissionFailureFixture } from "@/test-utils/image-admission-failure-fixture";
import {
  currentRoleImageAdmissionFailure,
  assertRoleImageAdmissionFailure,
} from "./admission-failure";

const state = vi.hoisted(() => ({ store: {} as Record<string, unknown> }));
vi.mock("./store", () => ({ useRoleImagesStore: () => state.store }));
vi.mock("@/features/platform/store", () => ({
  usePlatformStore: () => ({
    bootstrap: { organizationRef: "org_synthetic" },
    agents: {},
    roleEnvironments: {},
    roleImageRealtimeRevision: 0,
  }),
}));
vi.mock("vue-router", () => ({
  useRoute: () => ({ query: {} }),
  useRouter: () => ({ push: vi.fn() }),
}));
vi.mock("@/shared/locale", () => ({ currentLocale: () => "ru" }));
import RoleImageEditor from "./RoleImageEditor.vue";
import { i18n } from "@/app/i18n";

let recipe: RoleImageRecipe;
let build: RoleImageBuild;
function artifact(
  overrides: Partial<RoleImageArtifact> = {},
): RoleImageArtifact {
  return {
    scopeKind: recipe.scopeKind,
    organizationRef: recipe.organizationRef,
    projectRef: recipe.projectRef,
    ref: "artifact_synthetic",
    version: 1,
    recipeRef: recipe.ref,
    recipeGeneration: recipe.generation,
    buildRef: build.ref,
    manifestDigest: "a".repeat(64),
    provenanceSha256: "b".repeat(64),
    admissionVerdict: "ACCEPTED",
    promotionState: "PENDING",
    promotionRequested: false,
    declaredTools: [],
    verifiedToolInventory: unavailableInventoryFixture(),
    ...overrides,
  };
}
async function summary(
  value?: RoleImageArtifact,
  full = false,
  failure?: RoleImageAdmissionFailure,
): Promise<string> {
  state.store = {
    recipes: { [recipe.ref]: recipe },
    builds: { [recipe.ref]: [build] },
    artifacts: { [recipe.ref]: value },
    admissionFailures: { [recipe.ref]: failure },
    promotionReceipts: {},
    revisions: {},
    revisionNextPageToken: {},
    dependencies: {},
    createAllowed: {},
    environments: [],
    environmentByKey: new Map(),
    roleDefinitionByRef: new Map(),
    loadingDetail: false,
    mutating: false,
  };
  const props =
    recipe.scopeKind === "ORGANIZATION"
      ? {
          organizationScope: {
            kind: "ORGANIZATION" as const,
            organizationRef: recipe.organizationRef,
          },
        }
      : { projectRef: recipe.projectRef };
  const app = createSSRApp({
    render: () => h(RoleImageEditor, { ...props, recipeRef: recipe.ref }),
  }).use(i18n);
  app.component(
    "RouterLink",
    defineComponent({
      setup:
        (_props, { slots }) =>
        () =>
          h("a", slots.default?.()),
    }),
  );
  const html = await renderToString(app);
  if (full) return html;
  const start = html.indexOf('class="panel image-summary"');
  return html.slice(start, html.indexOf("</section>", start));
}
beforeEach(() => {
  i18n.global.locale.value = "ru";
  recipe = {
    scopeKind: "ORGANIZATION",
    organizationRef: "org_synthetic",
    ref: "image_synthetic",
    version: 1,
    projectRef: "",
    roleDefinitionRef: "role_synthetic",
    name: "kodex-selfdev",
    state: "ACTIVE",
    environment: { environmentKey: "standard" },
    sourceAvailable: false,
    generation: 1,
    promotedImageReady: false,
    nextActions: ["OPEN"],
    createdAt: "2026-10-04T00:00:00Z",
    updatedAt: "2026-10-04T00:00:00Z",
  };
  build = {
    scopeKind: "ORGANIZATION",
    organizationRef: recipe.organizationRef,
    projectRef: "",
    ref: "build_synthetic",
    version: 1,
    recipeRef: recipe.ref,
    recipeGeneration: 1,
    sourceAvailable: false,
    attempt: 1,
    stage: "COMPLETED",
    progressPercent: 100,
    createdAt: recipe.createdAt,
    updatedAt: recipe.updatedAt,
  };
});
describe("публичное состояние образа помощника", () => {
  it.each([
    "ADMISSION_WORKER_FAILED",
    "ADMISSION_LEASE_EXPIRED",
    "ADMISSION_EVIDENCE_ENTRY_EXCEEDS_BOUND",
    "ADMISSION_EVIDENCE_EXCEEDS_BOUND",
  ] as const)(
    "показывает авторитетный технический отказ %s отдельно от verdict",
    async (errorCode) => {
      for (const scopeKind of ["ORGANIZATION", "PROJECT"] as const) {
        recipe.scopeKind = scopeKind;
        recipe.projectRef = scopeKind === "PROJECT" ? "project_synthetic" : "";
        build.scopeKind = scopeKind;
        build.projectRef = recipe.projectRef;
        for (const locale of ["ru", "en"] as const) {
          i18n.global.locale.value = locale;
          const failure = imageAdmissionFailureFixture(recipe, build, {
            errorCode,
          });
          const html = await summary(undefined, true, failure);
          const start = html.indexOf('class="admission-failure"');
          expect(start).toBeGreaterThan(0);
          const notice = html.slice(start, html.indexOf("</section>", start));
          expect(notice).toContain('role="alert"');
          expect(notice).toContain(
            locale === "ru"
              ? "Проверка допуска завершилась с ошибкой"
              : "Image admission check failed",
          );
          expect(notice.slice(0, notice.indexOf("<details"))).not.toContain(
            errorCode,
          );
          expect(notice).toMatch(new RegExp(`<code[^>]*>${errorCode}</code>`));
          expect(notice).not.toMatch(/<details[^>]*\sopen(?:[\s=>])/);
          expect(html).not.toContain('data-state="REJECTED"');
          const lifecycle = html.slice(
            html.indexOf('class="image-lifecycle"'),
            html.indexOf('class="editor-layout"'),
          );
          expect(lifecycle).toContain(
            locale === "ru" ? "Сборка завершена" : "Build completed",
          );
          expect(lifecycle).toContain('data-state="FAILED"');
          expect(lifecycle).not.toContain(
            locale === "ru" ? "Ожидает проверки" : "Pending review",
          );
        }
      }
    },
  );
  it("не переносит старый отказ на новый build, attempt, generation или owner", async () => {
    const exact = imageAdmissionFailureFixture(recipe, build);
    expect(currentRoleImageAdmissionFailure(recipe, build, exact)).toBe(exact);
    for (const change of [
      { imageArtifactRef: "" },
      { version: 0 },
      { version: 1.5 },
      { recipeRef: "imgrec_foreign" },
      { recipeGeneration: 0 },
      { buildRef: "imgbld_old" },
      { buildAttempt: 0 },
      { organizationRef: "org_foreign" },
      { projectRef: "project_foreign" },
      { scopeKind: "PROJECT" },
      { state: "REJECTED" },
      { errorCode: "UNKNOWN_PRIVATE_CODE" },
    ]) {
      const foreign = { ...exact, ...change } as RoleImageAdmissionFailure;
      expect(
        currentRoleImageAdmissionFailure(recipe, build, foreign),
      ).toBeUndefined();
      expect(() =>
        assertRoleImageAdmissionFailure({
          recipe,
          builds: [build],
          admissionFailure: foreign,
        }),
      ).toThrow("failure identity");
      expect(await summary(undefined, true, foreign)).not.toContain(
        'class="admission-failure"',
      );
    }
    expect(
      currentRoleImageAdmissionFailure(
        recipe,
        { ...build, stage: "QUEUED" },
        exact,
      ),
    ).toBeUndefined();
    expect(
      currentRoleImageAdmissionFailure(
        { ...recipe, generation: 2 },
        build,
        exact,
      ),
    ).toBeUndefined();
    expect(() =>
      assertRoleImageAdmissionFailure({
        recipe,
        builds: [build],
        admissionFailure: exact,
        promotionCandidate: artifact(),
      }),
    ).toThrow();
  });
  it("старый опубликованный artifact не скрывает отказ новой сборки", async () => {
    recipe.promotedImageReady = true;
    const html = await summary(
      artifact({ buildRef: "imgbld_previous", recipeGeneration: 0 }),
      true,
      imageAdmissionFailureFixture(recipe, build),
    );
    expect(html).toContain('class="admission-failure"');
    const header = html.slice(
      html.indexOf('class="panel image-summary"'),
      html.indexOf("</section>"),
    );
    expect(header).toContain('data-state="FAILED"');
    expect(header).not.toContain('data-state="PROMOTED"');
  });
  it("называет ORGANIZATION образ помощником без name heuristic", async () => {
    recipe.name = "Среда аналитика";
    expect(await summary()).toContain("Образ помощника");
  });
  it("не угадывает PROJECT assistant по имени или managed lineage", async () => {
    recipe.scopeKind = "PROJECT";
    recipe.projectRef = "project_synthetic";
    recipe.managedLineage = {
      origin: "MANAGED",
      managedBy: "UI",
      sourceRef: "assistant",
      sourceRevision: "",
    };
    expect(await summary()).toContain("Образ ИИ-сотрудника");
  });
  it("COMPLETED без owner admission не обещает готовый образ", async () => {
    const html = await summary();
    expect(html).toContain("Ожидает допуска");
    expect(html).not.toContain("Готово");
    expect(html).not.toContain('data-state="PROMOTED"');
  });
  it("сохраняет completed build и 100% отдельно от допуска", async () => {
    const html = await summary(undefined, true);
    const lifecycle = html.slice(
      html.indexOf('class="image-lifecycle"'),
      html.indexOf('class="editor-layout"'),
    );
    expect(lifecycle).toContain("Сборка завершена");
    expect(lifecycle).toContain("100% ·");
    expect(lifecycle).not.toContain(">Готово<");
    expect(lifecycle.match(/Сборка завершена/g)).toHaveLength(1);
    expect(lifecycle.match(/Ожидает проверки/g)).toHaveLength(2);
  });
  it("допущенный но неопубликованный образ ожидает публикации", async () => {
    const html = await summary(artifact());
    expect(html).toContain("Ожидает публикации");
    expect(html).not.toContain("Готово");
  });
  it("отклонённый owner admission явно показывает отказ", async () => {
    const html = await summary(artifact({ admissionVerdict: "REJECTED" }));
    expect(html).toContain("Допуск отклонён");
    expect(html).toContain('data-state="REJECTED"');
  });
  it.each([
    { admissionVerdict: "REJECTED", promotionState: "PENDING" },
    { admissionVerdict: "ACCEPTED", promotionState: "REJECTED" },
    { admissionVerdict: "REJECTED", promotionState: "REJECTED" },
  ] as const)(
    "публикация текущего отклонённого образа явно заблокирована %o",
    async (state) => {
      for (const locale of ["ru", "en"] as const) {
        i18n.global.locale.value = locale;
        const html = await summary(artifact(state), true);
        const lifecycle = html.slice(
          html.indexOf('class="image-lifecycle"'),
          html.indexOf('class="editor-layout"'),
        );
        const promotion = lifecycle.slice(lifecycle.lastIndexOf("<article"));
        expect(promotion).toContain(
          locale === "ru"
            ? "Заблокирована допуском"
            : "Blocked by image admission",
        );
        expect(promotion).toContain('data-state="REJECTED"');
        expect(promotion).not.toContain(
          locale === "ru" ? "Ожидает проверки" : "Pending review",
        );
      }
    },
  );
  it("не переносит отказ предыдущего artifact/generation на текущую публикацию", async () => {
    for (const stale of [
      { buildRef: "build_previous" },
      { recipeGeneration: 0 },
    ]) {
      const html = await summary(
        artifact({
          ...stale,
          admissionVerdict: "REJECTED",
          promotionState: "REJECTED",
        }),
        true,
      );
      const lifecycle = html.slice(
        html.indexOf('class="image-lifecycle"'),
        html.indexOf('class="editor-layout"'),
      );
      const promotion = lifecycle.slice(lifecycle.lastIndexOf("<article"));
      expect(promotion).toContain("Ожидает проверки");
      expect(promotion).not.toContain("Заблокирована допуском");
    }
  });
  it("старый artifact не доказывает admission новой сборки", async () => {
    expect(
      await summary(
        artifact({ buildRef: "build_previous", recipeGeneration: 0 }),
      ),
    ).toContain("Ожидает допуска");
  });
  it("сворачивает полные digest в технические сведения, не скрывая verdict", async () => {
    const value = artifact({ admissionVerdict: "REJECTED" });
    const html = await summary(value, true);
    const card = html.slice(
      html.indexOf('class="panel artifact-card"'),
      html.indexOf("</section>", html.indexOf('class="panel artifact-card"')),
    );
    const detailsStart = card.indexOf("<details");
    expect(detailsStart).toBeGreaterThan(0);
    expect(card.slice(0, detailsStart)).toContain('data-state="REJECTED"');
    expect(card).toMatch(/<summary[^>]*>Технические сведения<\/summary>/);
    expect(card).not.toMatch(/<details[^>]*\sopen(?:[\s=>])/);
    const details = card.slice(detailsStart, card.indexOf("</details>"));
    expect(details).toContain(value.manifestDigest);
    expect(details).toContain(value.provenanceSha256);
  });
  it("допуск прежнего поколения не переносится на изменённый рецепт", async () => {
    recipe.generation = 2;
    expect(await summary(artifact({ recipeGeneration: 1 }))).toContain(
      "Ожидает допуска",
    );
  });
  it("PROMOTED artifact без owner readiness ещё не объявляет публикацию", async () => {
    const html = await summary(artifact({ promotionState: "PROMOTED" }));
    expect(html).toContain("Ожидает публикации");
    expect(html).not.toContain('data-state="PROMOTED"');
  });
  it("только authoritative promoted readback показывает публикацию", async () => {
    recipe.promotedImageReady = true;
    recipe.activeImageArtifactRef = "artifact_synthetic";
    const html = await summary(artifact({ promotionState: "PROMOTED" }));
    expect(html).toContain("Опубликован");
    expect(html).toContain('data-state="PROMOTED"');
  });
  it("сохраняет архив и активный build lifecycle", async () => {
    recipe.state = "ARCHIVED";
    expect(await summary()).toContain('data-state="ARCHIVED"');
    recipe.state = "ACTIVE";
    build.stage = "SOLVING";
    expect(await summary()).toContain('data-state="SOLVING"');
  });
});
