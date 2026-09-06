import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type {
  Agent,
  RevisionImpactPlan,
} from "@/shared/api/generated/openapi/types.gen";
const sdk = vi.hoisted(() => ({
  prepare: vi.fn(),
  publish: vi.fn(),
  read: vi.fn(),
}));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => ({
  prepareInstructionsImpact: sdk.prepare,
  commandAgentInstructions: sdk.publish,
  getAgent: sdk.read,
}));
vi.mock("@/shared/api/client", () => ({ requestSignal: () => undefined }));
import {
  prepareInstructionPublication,
  publishInstructions,
  readInstructionPublicationAgent,
} from "./instruction-publication";
const agent: Agent = {
  ref: "agent",
  version: 7,
  projectRef: "project",
  name: "Помощник",
  purpose: "",
  roleDescription: "",
  state: "READY",
  enabled: true,
  system: false,
  runtimeRef: "runtime",
  runtimeName: "Среда",
  runtimeReady: true,
  capabilities: [],
  integrations: [],
  knowledgeArtifactRefs: [],
  nextActions: ["PUBLISH"],
  updatedAt: "2026-09-06T00:00:00Z",
  draftInstructions: {
    ref: "draft",
    version: 3,
    revision: 2,
    state: "VALID",
    content: "Инструкция",
    validationMessages: [],
    createdAt: "2026-09-06T00:00:00Z",
  },
};
const plan: RevisionImpactPlan = {
  ref: "plan",
  version: 1,
  kind: "AGENT_INSTRUCTIONS",
  sourceRef: agent.ref,
  sourceVersion: agent.version,
  sourceRevisionRef: "previous",
  draftRef: "draft",
  draftVersion: 3,
  targetDigest: "target",
  digest: "digest",
  total: 0,
  state: "PREPARED",
  createdAt: "2026-09-06T00:00:00Z",
  expiresAt: "2099-09-06T00:00:00Z",
};
const response = (data: unknown) => ({ data, response: new Response(null) });
beforeEach(() => {
  vi.resetAllMocks();
  vi.stubGlobal("document", { cookie: `__Host-kodex-csrf=${"s".repeat(43)}` });
});
afterEach(() => vi.unstubAllGlobals());
it("Prepare не публикует и проверяет exact Agent/draft pin", async () => {
  sdk.prepare.mockResolvedValue(response(plan));
  await expect(prepareInstructionPublication(agent)).resolves.toEqual(plan);
  expect(sdk.publish).not.toHaveBeenCalled();
  sdk.prepare.mockResolvedValue(response({ ...plan, draftVersion: 4 }));
  await expect(prepareInstructionPublication(agent)).rejects.toThrow(
    "plan mismatch",
  );
});
it("передаёт исходный If-Match/key и явный пустой selection, возвращает минимальный receipt", async () => {
  const applied = {
    ...plan,
    state: "APPLIED",
    version: 2,
    publishedRevisionRef: "published",
  };
  sdk.publish.mockResolvedValue(
    response({
      agent: { ref: agent.ref, projectRef: agent.projectRef, version: 8 },
      plan: applied,
    }),
  );
  const key = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
  await expect(
    publishInstructions(agent, plan, [], key),
  ).resolves.toMatchObject({ plan: applied });
  expect(sdk.publish).toHaveBeenCalledWith(
    expect.objectContaining({
      body: { action: "PUBLISH", planRef: plan.ref, selectedItemRefs: [] },
      headers: {
        "If-Match": '"7"',
        "Idempotency-Key": key,
        "X-CSRF-Token": "s".repeat(43),
      },
    }),
  );
  expect(sdk.read).not.toHaveBeenCalled();
});
it("при неизвестном исходе не повторяет команду автоматически", async () => {
  sdk.publish.mockResolvedValue({
    error: { title: "Gateway Timeout", status: 504 },
    response: new Response(null, { status: 504 }),
  });
  await expect(
    publishInstructions(
      agent,
      plan,
      [],
      "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
    ),
  ).rejects.toMatchObject({ status: 504 });
  expect(sdk.publish).toHaveBeenCalledTimes(1);
});
it("отклоняет изменённый intent до команды и скрытый или чужой readback", async () => {
  await expect(
    publishInstructions({ ...agent, version: 8 }, plan, [], "key"),
  ).rejects.toThrow("intent changed");
  expect(sdk.publish).not.toHaveBeenCalled();
  sdk.read.mockResolvedValue(response({ ...agent, ref: "foreign" }));
  await expect(readInstructionPublicationAgent(agent.ref)).rejects.toThrow(
    "agent mismatch",
  );
  sdk.read.mockResolvedValue({
    error: { title: "Not Found", status: 404 },
    response: new Response(null, { status: 404 }),
  });
  await expect(
    readInstructionPublicationAgent(agent.ref),
  ).rejects.toMatchObject({ status: 404 });
});
