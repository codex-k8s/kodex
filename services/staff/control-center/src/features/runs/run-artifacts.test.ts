import { describe, expect, it } from "vitest";
import type { Run, RunGraph } from "@/shared/api/generated/openapi/types.gen";
import { hydrateRunArtifacts, runArtifactReferences } from "./run-artifacts";

describe("точные файлы снимка запуска", () => {
  it("объединяет ссылки корня и дочерних узлов без дубликатов", () => {
    expect(
      runArtifactReferences(
        {
          ref: "run_root",
          rootRunRef: "run_root",
          artifactRefs: ["art_root"],
        } as Run,
        {
          runRef: "run_root",
          nodes: [{ artifactRefs: ["art_child", "art_root"] }],
        } as RunGraph,
      ),
    ).toEqual(["art_root", "art_child"]);
  });
  it("не принимает чужой граф или недопустимую ссылку", () => {
    const run = {
      ref: "run_root",
      rootRunRef: "run_root",
      artifactRefs: [],
    } as unknown as Run;
    expect(() =>
      runArtifactReferences(run, {
        runRef: "run_foreign",
        nodes: [],
      } as unknown as RunGraph),
    ).toThrow("identity");
    expect(() =>
      runArtifactReferences({ ...run, artifactRefs: ["bad!"] }, {
        runRef: "run_root",
        nodes: [],
      } as unknown as RunGraph),
    ).toThrow("invalid");
  });
  it("читает все ссылки, одновременно не больше четырёх", async () => {
    let active = 0,
      peak = 0;
    const read: string[] = [];
    await hydrateRunArtifacts(
      Array.from({ length: 13 }, (_, i) => `art_${String(i)}`),
      async (ref) => {
        active++;
        peak = Math.max(peak, active);
        await Promise.resolve();
        read.push(ref);
        active--;
      },
      new AbortController().signal,
    );
    expect(peak).toBe(4);
    expect(read).toHaveLength(13);
  });
  it("не начинает новые чтения после отмены", async () => {
    const controller = new AbortController();
    controller.abort();
    let calls = 0;
    await expect(
      hydrateRunArtifacts(
        ["art_root"],
        () => {
          calls++;
          return Promise.resolve();
        },
        controller.signal,
      ),
    ).rejects.toThrow();
    expect(calls).toBe(0);
  });
});
