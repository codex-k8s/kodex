import { describe, expect, it, vi } from "vitest";

import { executeRuntimeSecretReveal } from "./reveal-flow";

describe("runtime secret reveal flow", () => {
  it("не вызывает reveal до fresh OIDC re-auth", async () => {
    const beginRuntimeSecretRevealReauth = vi.fn(() => Promise.resolve());
    const reveal = vi.fn(() =>
      Promise.resolve({
        value: "must-not-be-read",
        valueType: "STRING" as const,
      }),
    );

    const result = await executeRuntimeSecretReveal({
      projectRef: "project_sales",
      secretRef: "secret_main",
      session: {
        beginRuntimeSecretRevealReauth,
        consumePendingRuntimeSecretReveal: () => false,
        refreshMetadata: vi.fn(),
      },
      reveal,
    });

    expect(result).toEqual({ kind: "reauthentication-started" });
    expect(beginRuntimeSecretRevealReauth).toHaveBeenCalledWith({
      projectRef: "project_sales",
      secretRef: "secret_main",
    });
    expect(reveal).not.toHaveBeenCalled();
  });

  it("вызывает reveal только после атомарного потребления допуска", async () => {
    const refreshMetadata = vi.fn(() => Promise.resolve());
    const reveal = vi.fn(() =>
      Promise.resolve({ value: "one-use-value", valueType: "STRING" as const }),
    );

    const result = await executeRuntimeSecretReveal({
      projectRef: "project_sales",
      secretRef: "secret_main",
      session: {
        beginRuntimeSecretRevealReauth: vi.fn(),
        consumePendingRuntimeSecretReveal: () => true,
        refreshMetadata,
      },
      reveal,
    });

    expect(result).toEqual({
      kind: "revealed",
      value: { value: "one-use-value", valueType: "STRING" },
    });
    expect(reveal).toHaveBeenCalledOnce();
    expect(reveal).toHaveBeenCalledWith("secret_main");
    expect(refreshMetadata).toHaveBeenCalledOnce();
  });

  it("перечитывает session metadata и после неизвестного исхода Reveal без повторного эффекта", async () => {
    const refreshMetadata = vi.fn(() => Promise.resolve());
    const reveal = vi.fn(() =>
      Promise.reject(new Error("Unknown reveal outcome")),
    );
    await expect(
      executeRuntimeSecretReveal({
        projectRef: "project_sales",
        secretRef: "secret_main",
        session: {
          beginRuntimeSecretRevealReauth: vi.fn(),
          consumePendingRuntimeSecretReveal: () => true,
          refreshMetadata,
        },
        reveal,
      }),
    ).rejects.toThrow("Unknown reveal outcome");
    expect(reveal).toHaveBeenCalledOnce();
    expect(refreshMetadata).toHaveBeenCalledOnce();
  });
});
