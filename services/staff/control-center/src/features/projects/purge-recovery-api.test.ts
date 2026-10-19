import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  loadProjectPurgeRecovery,
  resolveProjectPurgeRecovery,
} from "./purge-recovery-api";
import type { ProjectPurgeRecovery } from "@/shared/api/generated/openapi/types.gen";

const sdk = vi.hoisted(() => ({
  getProjectPurgeRecovery: vi.fn(),
  resolveProjectPurgeRecovery: vi.fn(),
  mutation: vi.fn(),
}));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => sdk);
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal: AbortSignal) => signal,
}));
vi.mock("@/shared/api/problem", () => ({
  unwrap: (result: unknown) => result,
}));
vi.mock("@/shared/api/mutation", () => ({
  mutate: (
    request: (headers: object) => unknown,
    version: number,
    key: string,
  ) => {
    sdk.mutation(version, key);
    return request({
      "Idempotency-Key": key,
      "X-CSRF-Token": "synthetic-csrf",
      "If-Match": `"${String(version)}"`,
    });
  },
}));

beforeEach(() => {
  vi.resetAllMocks();
});
describe("Generated boundary подтверждаемого удаления", () => {
  it("preview идёт штатным SDK с bounded cancellation", async () => {
    const signal = new AbortController().signal;
    sdk.getProjectPurgeRecovery.mockResolvedValue({ data: {} });
    await loadProjectPurgeRecovery("prj_synthetic", signal);
    expect(sdk.getProjectPurgeRecovery).toHaveBeenCalledWith({
      path: { projectRef: "prj_synthetic" },
      signal,
    });
  });
  it("POST связывает exact preview/OCC/idempotency/CSRF без ложного project authority", async () => {
    const value = {
      projectRef: "prj_synthetic",
      version: 7,
      snapshotDigest: "a".repeat(64),
    } as ProjectPurgeRecovery;
    const signal = new AbortController().signal;
    sdk.resolveProjectPurgeRecovery.mockResolvedValue({ data: value });
    expect(
      await resolveProjectPurgeRecovery(value, "synthetic-key", signal),
    ).toEqual(value);
    expect(sdk.mutation).toHaveBeenCalledWith(7, "synthetic-key");
    expect(sdk.resolveProjectPurgeRecovery).toHaveBeenCalledWith({
      path: { projectRef: "prj_synthetic" },
      body: {
        snapshotDigest: value.snapshotDigest,
        acknowledgeUnknownOutcome: true,
      },
      headers: {
        "Idempotency-Key": "synthetic-key",
        "X-CSRF-Token": "synthetic-csrf",
        "If-Match": '"7"',
      },
      signal,
    });
  });
});
