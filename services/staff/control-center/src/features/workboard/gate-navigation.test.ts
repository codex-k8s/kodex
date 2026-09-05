import { describe, expect, it, vi } from "vitest";
const sdk = vi.hoisted(() => ({ getOwnerGate: vi.fn() }));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => sdk);
vi.mock("@/shared/api/client", () => ({
  requestSignal: () => new AbortController().signal,
}));
import { gateSelection, readAddressedGate } from "./gate-navigation";
import type { OwnerGate } from "@/shared/api/generated/openapi/types.gen";
const gate: OwnerGate = {
  ref: "gate_addressed",
  version: 2,
  projectRef: "project_addressed",
  runRef: "run_addressed",
  nodeRef: "node_addressed",
  title: "Решение",
  contextSummary: "Контекст",
  consequencesSummary: "Последствия",
  requestedBy: { ref: "user_addressed", displayName: "Владелец" },
  state: "OPEN",
  allowedDecisions: [],
  decisionConsequences: [],
  openedAt: "2026-09-05T00:00:00Z",
  nextActions: [],
};
describe("owner gate navigation", () => {
  it("не подставляет другое решение до exact read, даже если адресат был в старом списке", () => {
    expect(gateSelection(["other"], "", "wanted")).toBe("");
    expect(gateSelection(["wanted"], "wanted", "wanted")).toBe("");
    expect(gateSelection(["other", "wanted"], "wanted", "")).toBe("wanted");
    expect(gateSelection(["other"], "", "")).toBe("other");
  });
  it("читает exact ref и отклоняет несовпадающий owner scope", async () => {
    sdk.getOwnerGate.mockResolvedValue({
      data: gate,
      response: new Response(),
    });
    await expect(
      readAddressedGate(
        gate.ref,
        gate.projectRef,
        new AbortController().signal,
      ),
    ).resolves.toEqual(gate);
    expect(sdk.getOwnerGate).toHaveBeenLastCalledWith(
      expect.objectContaining({
        path: { gateRef: gate.ref },
        cache: "no-store",
      }),
    );
    await expect(
      readAddressedGate(
        gate.ref,
        "different_project",
        new AbortController().signal,
      ),
    ).rejects.toThrow("Invalid owner gate readback");
  });
  it("не принимает поздний ACK после смены страницы", async () => {
    const controller = new AbortController();
    sdk.getOwnerGate.mockImplementation(() => {
      controller.abort();
      return Promise.resolve({ data: gate, response: new Response() });
    });
    await expect(
      readAddressedGate(gate.ref, undefined, controller.signal),
    ).rejects.toMatchObject({ name: "AbortError" });
  });
});
