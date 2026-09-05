import { beforeEach, expect, it, vi } from "vitest";
import type {
  RuntimeSecretDraft,
  RuntimeSecretDraftImpactPlan,
  RuntimeSecretDraftImpactPage,
} from "@/shared/api/generated/openapi/types.gen";
const sdk = vi.hoisted(() => ({
  prepareRuntimeSecretDraftImpact: vi.fn(),
  getRuntimeSecretDraftImpact: vi.fn(),
  publishRuntimeSecretDraft: vi.fn(),
}));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => sdk);
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal?: AbortSignal) => signal,
}));
vi.mock("@/shared/api/mutation", () => ({
  mutate: async (
    request: (headers: Record<string, string>) => Promise<unknown>,
    version: number,
    key: string,
  ) =>
    request({
      "If-Match": `"${String(version)}"`,
      "Idempotency-Key": key,
      "X-CSRF-Token": "csrf",
    }),
}));
import {
  checkedPlan,
  readDraftImpact,
  publishSecretDraft,
} from "./draft-impact";
const draft: RuntimeSecretDraft = {
  ref: "draft",
  projectRef: "project",
  secretRef: "secret",
  version: 2,
  secretVersion: 1,
  generation: 1,
  name: "TOKEN",
  description: "",
  valueType: "STRING",
  state: "VALID",
  publishedRevision: 0,
  createdAt: "2026-09-05T00:00:00Z",
  updatedAt: "2026-09-05T00:00:00Z",
  expiresAt: "2026-09-06T00:00:00Z",
};
const plan: RuntimeSecretDraftImpactPlan = {
  ref: "plan",
  draftRef: "draft",
  secretRef: "secret",
  draftVersion: 2,
  secretVersion: 1,
  sourceRevision: 0,
  digest: "a".repeat(64),
  total: 3,
  state: "PREPARED",
  expiresAt: draft.expiresAt,
};
const page: RuntimeSecretDraftImpactPage = {
  plan,
  total: 1,
  nextPageToken: "",
  items: [
    {
      ref: "item",
      outcome: "PENDING",
      consumer: {
        environmentRef: "environment",
        environmentVersionRef: "revision",
        environmentVersion: 2,
        projectRef: "project",
        secretRevisions: [],
      },
    },
  ],
};
beforeEach(() => vi.clearAllMocks());
it("разделяет immutable total плана и текущую eligibility выдачи, сохраняет environment-only", async () => {
  sdk.getRuntimeSecretDraftImpact.mockResolvedValue({
    data: page,
    response: new Response(),
  });
  const controller = new AbortController();
  const result = await readDraftImpact(
    plan,
    controller.signal,
    " env ",
    "cursor",
  );
  expect(result.plan.total).toBe(3);
  expect(result.total).toBe(1);
  expect(result.items[0]?.consumer.consumer).toBeUndefined();
  expect(sdk.getRuntimeSecretDraftImpact.mock.calls[0]?.[0]).toMatchObject({
    query: { query: "env", pageToken: "cursor", pageSize: 40 },
    signal: controller.signal,
  });
});
it("отклоняет чужие pins, подмену digest, повтор cursor и неизвестные outcomes", async () => {
  expect(() => checkedPlan({ ...plan, secretVersion: 2 }, draft)).toThrow();
  for (const invalid of [
    { ...page, plan: { ...plan, digest: "other" } },
    { ...page, nextPageToken: "cursor" },
    { ...page, items: [{ ...page.items[0], outcome: "DONE" }] },
  ]) {
    sdk.getRuntimeSecretDraftImpact.mockResolvedValue({
      data: invalid,
      response: new Response(),
    });
    await expect(
      readDraftImpact(plan, new AbortController().signal, "", "cursor"),
    ).rejects.toThrow();
  }
});
it("публикует explicit empty selection с точными OCC/key и проверяет receipt", async () => {
  sdk.publishRuntimeSecretDraft.mockResolvedValue({
    data: {
      draft: { ...draft, state: "PUBLISHED", version: 3, publishedRevision: 1 },
      secret: {
        ref: "secret",
        projectRef: "project",
        version: 2,
        name: "TOKEN",
        description: "",
        state: "ACTIVE",
        valueType: "STRING",
        currentRevision: 1,
        nextActions: [],
        createdAt: draft.createdAt,
        updatedAt: draft.updatedAt,
      },
    },
  });
  await publishSecretDraft(draft, plan, [], "original-key");
  expect(sdk.publishRuntimeSecretDraft.mock.calls[0]?.[0]).toMatchObject({
    headers: { "If-Match": '"2"', "Idempotency-Key": "original-key" },
    body: {
      expectedSecretVersion: 1,
      impactPlanRef: "plan",
      selectedItemRefs: [],
    },
  });
  await expect(
    publishSecretDraft(draft, plan, ["item", "item"], "key"),
  ).rejects.toThrow();
  expect(sdk.publishRuntimeSecretDraft).toHaveBeenCalledOnce();
});
it("выдаёт APPLIED результаты с новой environment revision и сохраняет отказы", async () => {
  const applied = {
    ...page,
    plan: { ...plan, state: "APPLIED" },
    total: 3,
    items: [
      {
        ...page.items[0],
        outcome: "APPLIED",
        resultEnvironmentVersionRef: "new_revision",
      },
      { ...page.items[0], ref: "other", outcome: "FORBIDDEN" },
      { ...page.items[0], ref: "conflict", outcome: "CONFLICT" },
    ],
  };
  sdk.getRuntimeSecretDraftImpact.mockResolvedValue({
    data: applied,
    response: new Response(),
  });
  const result = await readDraftImpact(plan, new AbortController().signal);
  expect(result.items.map((item) => item.outcome)).toEqual([
    "APPLIED",
    "FORBIDDEN",
    "CONFLICT",
  ]);
});
