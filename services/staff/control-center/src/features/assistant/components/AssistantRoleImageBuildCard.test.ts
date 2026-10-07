import {
  createRenderer,
  defineComponent,
  h,
  nextTick,
  reactive,
  ssrContextKey,
  type App,
  type Ref,
  type SetupContext,
} from "vue";
import { createI18n } from "vue-i18n";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type {
  AssistantPlan,
  RoleImageAdmissionFailure,
  RoleImageRecipeDetail,
} from "@/shared/api/generated/openapi/types.gen";
import { imageAdmissionFailureFixture } from "@/test-utils/image-admission-failure-fixture";
import { unavailableInventoryFixture } from "@/test-utils/image-inventory-fixture";

const api = vi.hoisted(() => ({
  read: vi.fn(),
  command: vi.fn(),
  promote: vi.fn(),
}));
const platform = reactive({
  bootstrap: { organizationRef: "org_synthetic" },
  roleImageRealtimeRevision: 0,
  organizationRoleImageRealtimeRevision: 0,
});
vi.mock("@/features/platform/store", () => ({
  usePlatformStore: () => platform,
}));
vi.mock("@/features/role-images/api", () => ({
  loadRoleImageDetail: api.read,
  commandRoleImage: api.command,
  promoteRoleImageArtifact: api.promote,
}));
vi.mock("@/shared/ui/server-message", () => ({
  useServerMessage: () => (value: string) => value,
}));
import Card from "./AssistantRoleImageBuildCard.vue";

function appliedPlan(system: boolean): AssistantPlan {
  const pins = {
    scopeKind: "ORGANIZATION",
    organizationRef: "org_synthetic",
    systemAssistantRef: "agt_system",
  };
  return {
    ref: "pln_synthetic",
    version: 3,
    revision: 2,
    state: "APPLIED",
    conversationRef: "conv_synthetic",
    ...(system ? {} : { projectRef: "prj_synthetic" }),
    operations: [
      {
        ref: "op_synthetic",
        type: system
          ? "CREATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE"
          : "CREATE_ROLE_IMAGE_RECIPE",
        action: "CREATE",
        title: "Образ",
        summary: "Создать образ",
        target: { kind: "ROLE_IMAGE_RECIPE", name: "Образ" },
        parameters: system ? pins : {},
        before: {},
        after: system ? pins : {},
        selected: true,
        permitted: true,
        validationProblems: [],
      },
    ],
    auditSummary: "Создать образ",
    applied: true,
    contentDigest: "a".repeat(64),
    validationProblems: [],
    nextActions: [],
    receipt: {
      ref: "rct_synthetic",
      planRef: "pln_synthetic",
      planRevision: 2,
      outcome: "APPLIED",
      operationReceipts: [
        {
          operationRef: "op_synthetic",
          resourceRef: "imgrec_synthetic",
          outcome: "APPLIED",
          auditRef: "aud_synthetic",
        },
      ],
      conflicts: [],
      auditRefs: [],
      createdResourceRefs: [],
      createdAt: "2026-10-05T10:00:00Z",
    },
  };
}
function detail(system: boolean): RoleImageRecipeDetail {
  return {
    recipe: {
      ref: "imgrec_synthetic",
      name: "Образ",
      scopeKind: system ? "ORGANIZATION" : "PROJECT",
      organizationRef: "org_synthetic",
      projectRef: system ? "" : "prj_synthetic",
      sourceAvailable: true,
      version: 3,
      roleDefinitionRef: "role_synthetic",
      state: "ACTIVE",
      environment: {
        environmentKey: "standard",
        dockerfile: "FROM scratch",
      },
      generation: 3,
      promotedImageReady: false,
      nextActions: ["OPEN"],
      createdAt: "2026-10-05T10:00:00Z",
      updatedAt: "2026-10-05T10:00:00Z",
    },
    builds: [],
  };
}
interface State {
  loading: Ref<boolean>;
  problem: Ref<boolean>;
  detail: Ref<RoleImageRecipeDetail | undefined>;
  admissionFailure: Ref<RoleImageAdmissionFailure | undefined>;
  admissionRejected: Ref<boolean>;
  awaitingAdmission: Ref<boolean>;
  promotionState: Ref<string>;
}
const apps: App[] = [];
async function settle(): Promise<void> {
  for (let index = 0; index < 4; index++) await nextTick();
}
function mountCard(system: boolean): State {
  let state!: State;
  const renderer = createRenderer<Record<string, never>, Record<string, never>>(
    {
      patchProp() {},
      insert() {},
      remove() {},
      createElement: () => ({}),
      createText: () => ({}),
      createComment: () => ({}),
      setText() {},
      setElementText() {},
      parentNode: () => null,
      nextSibling: () => null,
    },
  );
  const setup = (
    Card as unknown as {
      setup: (props: Record<string, unknown>, context: SetupContext) => State;
    }
  ).setup;
  // Смонтированный setup сохраняет настоящий watcher lifetime до unmount.
  const app = renderer.createApp(
    defineComponent({
      setup(_props, context) {
        state = setup(
          { plan: appliedPlan(system), operationRef: "op_synthetic" },
          context,
        );
        return () => h("harness");
      },
    }),
  );
  app.use(createI18n({ legacy: false, locale: "ru" }));
  app.provide(ssrContextKey, { modules: new Set<string>() });
  app.mount({});
  apps.push(app);
  return state;
}
function snapshot(): void {
  platform.roleImageRealtimeRevision++;
  platform.organizationRoleImageRealtimeRevision++;
}
describe("Realtime-чтение карточки образа", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    platform.roleImageRealtimeRevision = 0;
    platform.organizationRoleImageRealtimeRevision = 0;
  });
  afterEach(() => {
    for (const app of apps.splice(0)) app.unmount();
  });

  it.each([true, false])(
    "closed securityREJECTED отдельно от promotionfailure, SYSTEM=%s",
    async (system) => {
      const value = detail(system);
      const build = {
        scopeKind: value.recipe.scopeKind,
        organizationRef: value.recipe.organizationRef,
        projectRef: value.recipe.projectRef,
        ref: "imgbld_completed",
        version: 1,
        recipeRef: value.recipe.ref,
        recipeGeneration: value.recipe.generation,
        sourceAvailable: false,
        attempt: 1,
        stage: "COMPLETED" as const,
        progressPercent: 100,
        createdAt: value.recipe.createdAt,
        updatedAt: value.recipe.updatedAt,
      };
      const artifact = {
        scopeKind: value.recipe.scopeKind,
        organizationRef: value.recipe.organizationRef,
        projectRef: value.recipe.projectRef,
        ref: "imgart_rejected",
        version: 1,
        recipeRef: value.recipe.ref,
        recipeGeneration: value.recipe.generation,
        buildRef: build.ref,
        manifestDigest: "a".repeat(64),
        provenanceSha256: "b".repeat(64),
        admissionVerdict: "REJECTED" as const,
        promotionState: "REJECTED" as const,
        promotionRequested: false,
        declaredTools: [],
        verifiedToolInventory: unavailableInventoryFixture(),
      };
      api.read.mockResolvedValue({
        ...value,
        builds: [build],
        promotionCandidate: artifact,
      });
      const state = mountCard(system);
      await settle();
      expect(state.admissionRejected.value).toBe(true);
      expect(state.promotionState.value).toBe("REJECTED");
      expect(state.admissionFailure.value).toBeUndefined();
      api.read.mockResolvedValue({
        ...value,
        builds: [build],
        promotionCandidate: {
          ...artifact,
          admissionVerdict: "ACCEPTED",
          promotionRequested: true,
        },
      });
      snapshot();
      await settle();
      expect(state.admissionRejected.value).toBe(false);
      expect(state.promotionState.value).toBe("FAILED");
    },
  );

  it.each([true, false])(
    "technical failure из WS-readback не остаётся pending, SYSTEM=%s",
    async (system) => {
      const value = detail(system);
      value.builds = [
        {
          scopeKind: value.recipe.scopeKind,
          organizationRef: value.recipe.organizationRef,
          projectRef: value.recipe.projectRef,
          ref: "imgbld_completed",
          version: 1,
          recipeRef: value.recipe.ref,
          recipeGeneration: value.recipe.generation,
          sourceAvailable: false,
          attempt: 1,
          stage: "COMPLETED",
          progressPercent: 100,
          createdAt: value.recipe.createdAt,
          updatedAt: value.recipe.updatedAt,
        },
      ];
      api.read.mockResolvedValue(value);
      const state = mountCard(system);
      await settle();
      expect(state.awaitingAdmission.value).toBe(true);
      const completedBuild = value.builds[0];
      if (!completedBuild)
        throw new Error("Synthetic completed build is absent");
      const failure = imageAdmissionFailureFixture(
        value.recipe,
        completedBuild,
        { errorCode: "ADMISSION_LEASE_EXPIRED" },
      );
      api.read.mockResolvedValue({ ...value, admissionFailure: failure });
      snapshot();
      await settle();
      expect(state.problem.value).toBe(false);
      expect(state.admissionFailure.value).toEqual(failure);
      expect(state.awaitingAdmission.value).toBe(false);
      expect(state.promotionState.value).toBe("FAILED");
      api.read.mockResolvedValue({
        ...value,
        builds: [{ ...completedBuild, ref: "imgbld_new", stage: "QUEUED" }],
      });
      snapshot();
      await settle();
      expect(state.admissionFailure.value).toBeUndefined();
      expect(state.promotionState.value).toBe("PENDING");
    },
  );

  it.each([true, false])(
    "один snapshot/rejoin вызывает одно чтение, SYSTEM=%s",
    async (system) => {
      api.read.mockResolvedValue(detail(system));
      const state = mountCard(system);
      await settle();
      expect(api.read).toHaveBeenCalledTimes(1);
      expect(state.problem.value).toBe(false);
      snapshot();
      await settle();
      expect(api.read).toHaveBeenCalledTimes(2);
      snapshot();
      await settle();
      expect(api.read).toHaveBeenCalledTimes(3);
      expect(state.detail.value?.recipe.ref).toBe("imgrec_synthetic");
      const address = system
        ? { kind: "ORGANIZATION", organizationRef: "org_synthetic" }
        : "prj_synthetic";
      expect(api.read).toHaveBeenLastCalledWith(
        address,
        "imgrec_synthetic",
        expect.any(AbortSignal),
      );
    },
  );
  it.each([true, false])(
    "не читает на изменение чужой области, SYSTEM=%s",
    async (system) => {
      api.read.mockResolvedValue(detail(system));
      mountCard(system);
      await settle();
      if (system) platform.roleImageRealtimeRevision++;
      else platform.organizationRoleImageRealtimeRevision++;
      await settle();
      expect(api.read).toHaveBeenCalledTimes(1);
      if (system) platform.organizationRoleImageRealtimeRevision++;
      else platform.roleImageRealtimeRevision++;
      await settle();
      expect(api.read).toHaveBeenCalledTimes(2);
    },
  );
  it("сохраняет одно повторное чтение после snapshot во время запроса", async () => {
    let resolve!: (value: RoleImageRecipeDetail) => void;
    api.read
      .mockImplementationOnce(
        () =>
          new Promise<RoleImageRecipeDetail>((done) => {
            resolve = done;
          }),
      )
      .mockResolvedValue(detail(true));
    const state = mountCard(true);
    snapshot();
    await nextTick();
    snapshot();
    await nextTick();
    expect(api.read).toHaveBeenCalledTimes(1);
    resolve(detail(true));
    await settle();
    expect(api.read).toHaveBeenCalledTimes(2);
    expect(state.loading.value).toBe(false);
  });
  it("после unmount не выполняет отложенное чтение", async () => {
    let resolve!: (value: RoleImageRecipeDetail) => void;
    api.read.mockImplementationOnce(
      () =>
        new Promise<RoleImageRecipeDetail>((done) => {
          resolve = done;
        }),
    );
    mountCard(true);
    snapshot();
    await nextTick();
    apps.pop()?.unmount();
    resolve(detail(true));
    await settle();
    expect(api.read).toHaveBeenCalledTimes(1);
  });
});
