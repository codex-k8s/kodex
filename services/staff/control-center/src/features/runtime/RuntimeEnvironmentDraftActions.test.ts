import { beforeEach, expect, it, vi } from "vitest";
import type { Ref } from "vue";
import { captureSetupState } from "@/test-utils/setup-harness";
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
vi.mock("./environment-drafts", () => api);
vi.mock("@/shared/api/mutation", () => ({
  idempotencyKey: () => "00000000-0000-4000-8000-000000000001",
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
  pending: Ref<unknown>;
  problem: Ref<unknown>;
  save(): Promise<void>;
  validate(): Promise<void>;
  preview(): Promise<void>;
  publish(selected: string[]): Promise<void>;
};
beforeEach(() => {
  vi.clearAllMocks();
  const values = new Map<string, string>();
  vi.stubGlobal("window", {
    sessionStorage: {
      getItem: (key: string) => values.get(key) ?? null,
      setItem: (key: string, value: string) => values.set(key, value),
      removeItem: (key: string) => values.delete(key),
    },
  });
});
async function state(input = specification) {
  return (await captureSetupState(Component, undefined, {
    resourceScope: scope,
    environment,
    specification: input,
    canEdit: true,
    valid: true,
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
