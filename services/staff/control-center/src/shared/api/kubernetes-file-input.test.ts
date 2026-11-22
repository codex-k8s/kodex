import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { resetOwnerRequests } from "./owner-lifetime";
import { fileInputFixture } from "@/shared/lib/kubernetes-file-input.test-support";
interface Request {
  path: { invocationRef: string; sourceRef?: string };
  signal: AbortSignal;
  query?: Record<string, unknown>;
}
const sdk = vi.hoisted(() => ({
  listManagedKubernetesFileInputs:
    vi.fn<
      (request: Request) => Promise<{ data: unknown; response: Response }>
    >(),
  repairManagedKubernetesFileCleanup: vi.fn(),
  getManagedKubernetesFileInput:
    vi.fn<
      (request: Request) => Promise<{ data: unknown; response: Response }>
    >(),
}));
vi.mock("./generated/openapi/sdk.gen", () => sdk);
vi.mock("./client", () => ({ requestSignal: (signal: AbortSignal) => signal }));
import {
  listKubernetesFileInputs,
  readKubernetesFileInput,
  listKubernetesFileCleanup,
  readKubernetesFileCleanup,
  repairKubernetesFileCleanup,
} from "./kubernetes-file-input";
beforeEach(() => vi.resetAllMocks());
afterEach(() => resetOwnerRequests());
it("uses exact metadata locators and never submits raw file selectors", async () => {
  const value = fileInputFixture(),
    signal = new AbortController().signal;
  sdk.listManagedKubernetesFileInputs.mockResolvedValue({
    data: { projection: "SOURCE", items: [value] },
    response: new Response(),
  });
  sdk.getManagedKubernetesFileInput.mockResolvedValue({
    data: { projection: "SOURCE", view: value },
    response: new Response(),
  });
  expect(await listKubernetesFileInputs(value.invocationRef, signal)).toEqual([
    value,
  ]);
  expect(
    await readKubernetesFileInput(value.invocationRef, value.sourceRef, signal),
  ).toEqual(value);
  expect(sdk.listManagedKubernetesFileInputs.mock.calls[0]?.[0]).toEqual({
    path: { invocationRef: value.invocationRef },
    signal,
  });
  expect(sdk.getManagedKubernetesFileInput.mock.calls[0]?.[0]).toEqual({
    path: { invocationRef: value.invocationRef, sourceRef: value.sourceRef },
    signal,
  });
});
it("rejects invalid selectors before network and discards a cancelled readback", async () => {
  const value = fileInputFixture(),
    controller = new AbortController();
  await expect(
    readKubernetesFileInput(
      value.invocationRef,
      "../foreign",
      controller.signal,
    ),
  ).rejects.toThrow();
  expect(sdk.getManagedKubernetesFileInput).not.toHaveBeenCalled();
  sdk.getManagedKubernetesFileInput.mockImplementation(() => {
    controller.abort();
    return Promise.resolve({
      data: { projection: "SOURCE", view: value },
      response: new Response(),
    });
  });
  await expect(
    readKubernetesFileInput(
      value.invocationRef,
      value.sourceRef,
      controller.signal,
    ),
  ).rejects.toThrow();
});
it("keeps cleanup-only reads free of source selectors and binds original receipt key", async () => {
  const signal = new AbortController().signal;
  const minimal = {
    sourceRef: "kfs_synthetic",
    invocationRef: "inv_synthetic",
    cleanup: {
      version: 3,
      repairGeneration: 0,
      outstandingCount: 1,
      outstandingBytes: 2048,
      exhaustedCount: 1,
      eligibleExhaustedCount: 1,
    },
  };
  sdk.listManagedKubernetesFileInputs.mockResolvedValue({
    data: { projection: "CLEANUP_ONLY", items: [minimal] },
    response: new Response(),
  });
  sdk.getManagedKubernetesFileInput.mockResolvedValue({
    data: { projection: "CLEANUP_ONLY", cleanupView: minimal },
    response: new Response(),
  });
  expect(
    await listKubernetesFileCleanup(minimal.invocationRef, signal),
  ).toEqual([minimal]);
  expect(
    (
      await readKubernetesFileCleanup(
        minimal.invocationRef,
        minimal.sourceRef,
        signal,
        "original-cleanup-key",
      )
    ).cleanupView,
  ).toEqual(minimal);
  expect(sdk.getManagedKubernetesFileInput).toHaveBeenLastCalledWith({
    path: {
      invocationRef: minimal.invocationRef,
      sourceRef: minimal.sourceRef,
    },
    query: { cleanupOnly: true, cleanupIdempotencyKey: "original-cleanup-key" },
    signal,
  });
  expect(sdk.repairManagedKubernetesFileCleanup).not.toHaveBeenCalled();
});
it("invalid cleanup authority or exhausted counters never reach the mutation client", async () => {
  const value = fileInputFixture(),
    signal = new AbortController().signal;
  await expect(
    repairKubernetesFileCleanup(
      value.invocationRef,
      {
        sourceRef: value.sourceRef,
        invocationRef: "inv_foreign00",
        cleanup: {
          version: 3,
          repairGeneration: 0,
          outstandingCount: 1,
          outstandingBytes: 2048,
          exhaustedCount: 1,
          eligibleExhaustedCount: 1,
        },
      },
      signal,
      "original-cleanup-key",
    ),
  ).rejects.toThrow();
  expect(sdk.repairManagedKubernetesFileCleanup).not.toHaveBeenCalled();
});
