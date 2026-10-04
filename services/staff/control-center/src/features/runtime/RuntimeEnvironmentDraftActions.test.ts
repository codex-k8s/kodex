import { initializeRuntimeOwnerFixture } from "@/test-utils/runtime-owner-fixture";
import { beforeEach, expect, it, vi } from "vitest";
import type { Ref } from "vue";
import { captureSetupState } from "@/test-utils/setup-harness";
import { createI18n } from "vue-i18n";
import type { PublicationAttempt } from "./publication-attempt";
import type {
  RuntimeEnvironmentDraft,
  RuntimeEnvironmentSet,
  RevisionImpactPlan,
} from "@/shared/api/generated/openapi/types.gen";
const api = vi.hoisted(() => ({
  createEnvironmentDraft: vi.fn(),
  saveEnvironmentDraft: vi.fn(),
  readEnvironmentDraft: vi.fn(),
  transitionEnvironmentDraft: vi.fn(),
  prepareEnvironmentPublication: vi.fn(),
  publishEnvironmentDraft: vi.fn(),
  environmentDraftFingerprint: (value: unknown) => JSON.stringify(value),
}));
const recovery = vi.hoisted(() => ({ restore: vi.fn(), confirm: vi.fn() }));
const mutation = vi.hoisted(() => ({ key: vi.fn() }));
vi.mock("./publication-impact", async (original) => ({
  ...(await original<typeof import("./publication-impact")>()),
  restorePublicationImpact: recovery.restore,
}));
vi.mock("@/shared/ui/confirmation", () => ({
  requestConfirmation: recovery.confirm,
}));
vi.mock("./environment-drafts", () => api);
vi.mock("@/shared/api/mutation", () => ({
  idempotencyKey: mutation.key,
}));
import Component from "./RuntimeEnvironmentDraftActions.vue";
const scope = { kind: "ORGANIZATION", organizationRef: "org_fixture" } as const;
const specification = {
  name: "Среда",
  description: "",
  imageArtifactRef: "artifact_fixture",
  tools: [],
  values: [],
  secretBindings: [],
};
const environment = {
  scopeKind: "ORGANIZATION",
  organizationRef: scope.organizationRef,
  projectRef: "",
  ref: "environment_fixture",
  version: 2,
} as RuntimeEnvironmentSet;
const draft = {
  scopeKind: "ORGANIZATION",
  organizationRef: scope.organizationRef,
  projectRef: "",
  ref: "draft_fixture",
  version: 1,
  state: "DRAFT",
  environmentRef: environment.ref,
  expectedEnvironmentVersion: 2,
  specification,
  diagnostics: [],
} as RuntimeEnvironmentDraft;
const plan = {
  ref: "plan_fixture",
  version: 1,
  kind: "RUNTIME_ENVIRONMENT",
  draftRef: draft.ref,
  draftVersion: 2,
  sourceRef: environment.ref,
  sourceVersion: 2,
  targetDigest: "target",
  digest: "digest",
  total: 0,
  state: "PREPARED",
  createdAt: "2026-10-04T00:00:00Z",
  expiresAt: "2099-10-04T00:00:00Z",
} as RevisionImpactPlan;
type State = {
  draft: Ref<RuntimeEnvironmentDraft | undefined>;
  plan: Ref<RevisionImpactPlan | undefined>;
  pending: Ref<PublicationAttempt | undefined>;
  missingPlan: Ref<boolean>;
  problem: Ref<unknown>;
  save(): Promise<void>;
  validate(): Promise<void>;
  preview(): Promise<void>;
  publish(selected: string[]): Promise<void>;
  reconcile(): Promise<void>;
  reprepareMissingPlan(): Promise<void>;
};
beforeEach(() => {
  vi.resetAllMocks();
  let sequence = 0;
  mutation.key.mockImplementation(
    () => `00000000-0000-4000-8000-${String(++sequence).padStart(12, "0")}`,
  );
  const values = new Map<string, string>();
  vi.stubGlobal("window", {
    sessionStorage: {
      getItem: (key: string) => values.get(key) ?? null,
      setItem: (key: string, value: string) => values.set(key, value),
      removeItem: (key: string) => values.delete(key),
    },
  });
});
it("NOT_FOUND impact требует явного нового подтверждения и не повторяет publish", async () => {
  const value = await state();
  const validated = {
    ...draft,
    state: "VALID" as const,
    version: 2,
    validationDigest: "a".repeat(64),
  };
  value.draft.value = validated;
  value.plan.value = plan;
  api.readEnvironmentDraft.mockResolvedValue(validated);
  api.publishEnvironmentDraft.mockRejectedValue(new Error("lost ACK"));
  await value.publish([]);
  const previousKey = value.pending.value?.key;
  recovery.restore.mockRejectedValue({
    status: 404,
    code: "RESOURCE_NOT_FOUND",
  });
  await value.reconcile();
  expect(value.missingPlan.value).toBe(true);
  expect(api.prepareEnvironmentPublication).not.toHaveBeenCalled();
  recovery.confirm.mockResolvedValue(false);
  await value.reprepareMissingPlan();
  expect(value.pending.value).toBeDefined();
  expect(api.prepareEnvironmentPublication).not.toHaveBeenCalled();
  recovery.confirm.mockResolvedValue(true);
  api.prepareEnvironmentPublication.mockResolvedValue({
    ...plan,
    ref: "new_plan_fixture",
  });
  await value.reprepareMissingPlan();
  expect(value.pending.value).toBeUndefined();
  expect(value.plan.value.ref).toBe("new_plan_fixture");
  expect(api.prepareEnvironmentPublication).toHaveBeenCalledWith(
    validated,
    expect.any(AbortSignal),
    expect.any(String),
  );
  expect(api.prepareEnvironmentPublication.mock.calls[0]?.[2]).not.toBe(
    previousKey,
  );
  expect(api.publishEnvironmentDraft).toHaveBeenCalledOnce();
});
it("NOT_FOUND уже опубликованного draft сверяет опубликованный ref без повторной публикации", async () => {
  const value = await state();
  const validated = {
    ...draft,
    state: "VALID" as const,
    version: 2,
    validationDigest: "a".repeat(64),
  };
  value.draft.value = validated;
  value.plan.value = plan;
  api.readEnvironmentDraft.mockResolvedValue(validated);
  api.publishEnvironmentDraft.mockRejectedValue(new Error("lost ACK"));
  await value.publish([]);
  recovery.restore.mockRejectedValue({
    status: 404,
    code: "RESOURCE_NOT_FOUND",
  });
  api.readEnvironmentDraft.mockResolvedValue({
    ...validated,
    state: "PUBLISHED",
    publishedEnvironmentRef: environment.ref,
    version: 3,
  });
  await value.reconcile();
  expect(value.pending.value).toBeUndefined();
  expect(value.missingPlan.value).toBe(false);
  expect(value.draft.value.state).toBe("PUBLISHED");
  expect(api.prepareEnvironmentPublication).not.toHaveBeenCalled();
  expect(api.publishEnvironmentDraft).toHaveBeenCalledOnce();
});
it("NOT_FOUND с новым validation digest не разрешает переподготовить старый intent", async () => {
  const value = await state();
  const validated = {
    ...draft,
    state: "VALID" as const,
    version: 2,
    validationDigest: "a".repeat(64),
  };
  value.draft.value = validated;
  value.plan.value = plan;
  api.readEnvironmentDraft.mockResolvedValue(validated);
  api.publishEnvironmentDraft.mockRejectedValue(new Error("lost ACK"));
  await value.publish([]);
  recovery.restore.mockRejectedValue({
    status: 404,
    code: "RESOURCE_NOT_FOUND",
  });
  api.readEnvironmentDraft.mockResolvedValue({
    ...validated,
    validationDigest: "b".repeat(64),
  });
  await value.reconcile();
  expect(value.missingPlan.value).toBe(false);
  expect(value.pending.value).toBeDefined();
  await value.reprepareMissingPlan();
  expect(api.prepareEnvironmentPublication).not.toHaveBeenCalled();
});
async function state(
  input = specification,
  extra: Record<string, unknown> = {},
) {
  const i18n = createI18n({
    legacy: false,
    locale: "ru",
    messages: {
      ru: {
        managed: { impact: "Влияние" },
        assistant: {
          resources: {
            reprepareImpact: "Проверить заново",
            reprepareImpactHelp: "Проверить заново с отдельным подтверждением",
          },
        },
      },
    },
  });
  return (await captureSetupState(Component, (app) => app.use(i18n), {
    resourceScope: scope,
    environment,
    specification: input,
    canEdit: true,
    valid: true,
    ...extra,
  })) as unknown as State;
}
it("Save draft не публикует среду и последовательно включает validation/impact", async () => {
  const value = await state();
  api.createEnvironmentDraft.mockResolvedValue(draft);
  await value.preview();
  expect(api.prepareEnvironmentPublication).not.toHaveBeenCalled();
  await value.save();
  expect(api.publishEnvironmentDraft).not.toHaveBeenCalled();
  api.transitionEnvironmentDraft.mockResolvedValue({
    ...draft,
    state: "VALID",
    version: 2,
  });
  await value.validate();
  api.prepareEnvironmentPublication.mockResolvedValue(plan);
  await value.preview();
  expect(value.plan.value).toEqual(plan);
  expect(api.publishEnvironmentDraft).not.toHaveBeenCalled();
});
it("чужой ORG receipt не записывается как текущий черновик", async () => {
  const value = await state();
  api.createEnvironmentDraft.mockResolvedValue({
    ...draft,
    organizationRef: "org_foreign",
  });
  await value.save();
  expect(value.draft.value).toBeUndefined();
  expect(value.problem.value).toBeDefined();
});
it("lost save ACK повторяется с тем же idempotency key", async () => {
  const value = await state();
  api.createEnvironmentDraft
    .mockRejectedValueOnce(new Error("lost ACK"))
    .mockResolvedValueOnce(draft);
  await value.save();
  await value.save();
  expect(api.createEnvironmentDraft.mock.calls[0]).toEqual(
    api.createEnvironmentDraft.mock.calls[1],
  );
});
it("публикация перечитывает точную version и не отправляет stale намерение", async () => {
  const value = await state();
  value.draft.value = { ...draft, state: "VALID", version: 2 };
  value.plan.value = plan;
  api.readEnvironmentDraft.mockResolvedValue({
    ...draft,
    state: "VALID",
    version: 3,
  });
  await value.publish([]);
  expect(api.publishEnvironmentDraft).not.toHaveBeenCalled();
  expect(value.problem.value).toBeDefined();
});
it("lost publication ACK блокирует новую mutation и сохраняет только metadata", async () => {
  const value = await state();
  value.draft.value = { ...draft, state: "VALID", version: 2 };
  value.plan.value = plan;
  api.readEnvironmentDraft.mockResolvedValue(value.draft.value);
  api.publishEnvironmentDraft.mockRejectedValue(new Error("lost ACK"));
  await value.publish([]);
  await value.publish([]);
  expect(api.publishEnvironmentDraft).toHaveBeenCalledOnce();
  expect(value.pending.value).toBeDefined();
  const stored = window.sessionStorage.getItem(
    `kodex.publication-attempt:RUNTIME_ENVIRONMENT:${draft.ref}`,
  );
  if (!stored) throw new Error("Publication intent fixture is missing");
  expect(stored).not.toContain("artifact_fixture");
  expect(stored).not.toContain("specification");
});

beforeEach(() => initializeRuntimeOwnerFixture("org_fixture"));

it("reload опубликованного собственного draft не требует прежнюю source version и не повторяет mutation", async () => {
  api.readEnvironmentDraft.mockResolvedValue({
    ...draft,
    state: "PUBLISHED",
    version: 3,
    publishedEnvironmentRef: environment.ref,
  });
  const value = await state(specification, {
    environment: { ...environment, version: 3 },
    initialDraftRef: draft.ref,
  });
  expect(value.draft.value?.state).toBe("PUBLISHED");
  expect(value.problem.value).toBeUndefined();
  await value.validate();
  await value.preview();
  await value.publish([]);
  expect(api.transitionEnvironmentDraft).not.toHaveBeenCalled();
  expect(api.prepareEnvironmentPublication).not.toHaveBeenCalled();
  expect(api.publishEnvironmentDraft).not.toHaveBeenCalled();
});

it("reload published draft не принимает чужой ref, scope или невозможную source version", async () => {
  for (const invalid of [
    { publishedEnvironmentRef: "environment_foreign" },
    { organizationRef: "org_foreign" },
    { expectedEnvironmentVersion: 3 },
  ]) {
    api.readEnvironmentDraft.mockResolvedValue({
      ...draft,
      state: "PUBLISHED",
      version: 3,
      publishedEnvironmentRef: environment.ref,
      ...invalid,
    });
    const value = await state(specification, {
      environment: { ...environment, version: 3 },
      initialDraftRef: draft.ref,
    });
    expect(value.draft.value).toBeUndefined();
    expect(value.problem.value).toBeDefined();
  }
  expect(api.publishEnvironmentDraft).not.toHaveBeenCalled();
});
