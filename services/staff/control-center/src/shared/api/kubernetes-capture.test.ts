import { describe, expect, it } from "vitest";
import {
  parseKubernetesCaptureReadback,
  parseKubernetesCaptureSummary,
  parseKubernetesCapturePage,
  repairKubernetesCapture,
  KubernetesCaptureProtocolError,
} from "./kubernetes-capture";
import type { KubernetesCaptureReadback } from "./generated/openapi/types.gen";
const context = {
  invocationRef: "inv_synthetic",
  runRef: "run_synthetic",
  nodeRef: "nod_synthetic",
};
export function captureFixture(): KubernetesCaptureReadback {
  return {
    view: {
      summary: {
        captureRef: "koc_synthetic",
        streamRef: "kst_synthetic",
        captureGeneration: 1,
        version: 5,
        state: "WAITING_OWNER",
        reasonCode: "CAPTURE_RETRY_EXHAUSTED",
      },
      ...context,
      operation: "kubernetes.exec",
      kind: "NONE",
      attemptCount: 3,
      repairGeneration: 0,
      sealedAt: "2026-11-16T02:00:00Z",
      artifacts: [],
    },
  };
}
describe("original-owner capture metadata", () => {
  it("reads history without Tool metadata and refuses foreign, duplicate or private results", () => {
    const original = captureFixture().view;
    expect(
      parseKubernetesCapturePage(
        { items: [original], nextPageToken: "" },
        context,
      ).items,
    ).toEqual([original]);
    for (const v of [
      { items: [{ ...original, runRef: "run_foreignxx" }], nextPageToken: "" },
      { items: [original, original], nextPageToken: "" },
      {
        items: [{ ...original, privateObjectKey: "private" }],
        nextPageToken: "",
      },
      { items: [], nextPageToken: original.summary.captureRef },
      { items: [original], nextPageToken: "koc_foreignxx" },
      { items: [original], nextPageToken: "", actorRef: "usr_foreignxx" },
    ])
      expect(() => parseKubernetesCapturePage(v, context)).toThrow(
        KubernetesCaptureProtocolError,
      );
  });
  it("rejects a mixed selected root before preparing a repair request", async () => {
    const v = captureFixture();
    await expect(
      repairKubernetesCapture(
        { ...context, nodeRef: "nod_foreignxx" },
        v.view,
        new AbortController().signal,
        "original-repair-key",
      ),
    ).rejects.toBeInstanceOf(KubernetesCaptureProtocolError);
  });
  it("discovers a waiting capture before an artifact exists", () => {
    const v = captureFixture();
    expect(
      parseKubernetesCaptureReadback(v, context, v.view.summary.captureRef),
    ).toEqual(v);
  });
  it("keeps a monotonic original repair receipt after publication", () => {
    const v = captureFixture();
    Object.assign(v.view.summary, {
      state: "COMPLETE",
      version: 8,
      reasonCode: "",
    });
    Object.assign(v.view, {
      kind: "PARTIAL",
      attemptCount: 4,
      repairGeneration: 1,
    });
    v.repairReceipt = {
      captureRef: v.view.summary.captureRef,
      repairGeneration: 1,
      version: 6,
      attemptCount: 3,
      acceptedAt: "2026-11-16T02:01:00Z",
    };
    expect(
      parseKubernetesCaptureReadback(v, context, v.view.summary.captureRef)
        .repairReceipt?.attemptCount,
    ).toBe(3);
  });
  it("rejects foreign scope, private fields, future state and mixed completion", () => {
    const original = captureFixture();
    const variants: unknown[] = [
      { ...original, taskBinding: {} },
      { view: { ...original.view, runRef: "run_foreignxx" } },
      { view: { ...original.view, kind: "FULL" } },
      {
        view: {
          ...original.view,
          summary: { ...original.view.summary, state: "FUTURE" },
        },
      },
      {
        view: {
          ...original.view,
          summary: { ...original.view.summary, privateObjectKey: "private" },
        },
      },
      {
        ...original,
        repairReceipt: {
          captureRef: original.view.summary.captureRef,
          repairGeneration: 1,
          version: 6,
          attemptCount: 3,
          acceptedAt: "2026-11-16T02:01:00Z",
        },
      },
    ];
    for (const v of variants)
      expect(() =>
        parseKubernetesCaptureReadback(
          v,
          context,
          original.view.summary.captureRef,
        ),
      ).toThrow();
    expect(() =>
      parseKubernetesCaptureSummary({
        ...original.view.summary,
        reasonCode: "upstream private diagnostic",
      }),
    ).toThrow();
  });
});

it("refuses cleanup counters and receipts that invent quota release or foreign repair", () => {
  const original = captureFixture();
  original.view.cleanup = {
    version: 2,
    repairGeneration: 1,
    outstandingCount: 2,
    outstandingBytes: 30,
    exhaustedCount: 1,
    eligibleExhaustedCount: 1,
  };
  original.cleanupReceipt = {
    captureRef: original.view.summary.captureRef,
    version: 2,
    repairGeneration: 1,
    repairedCount: 1,
    remainingExhaustedCount: 0,
    acceptedAt: "2026-11-16T02:00:01Z",
  };
  expect(
    parseKubernetesCaptureReadback(
      original,
      context,
      original.view.summary.captureRef,
    ),
  ).toEqual(original);
  for (const mutate of [
    (v: KubernetesCaptureReadback) => {
      if (!v.view.cleanup) throw new Error("cleanup fixture unavailable");
      v.view.cleanup.eligibleExhaustedCount = 3;
    },
    (v: KubernetesCaptureReadback) => {
      if (!v.view.cleanup) throw new Error("cleanup fixture unavailable");
      v.view.cleanup.outstandingBytes = -1;
    },
    (v: KubernetesCaptureReadback) => {
      if (!v.cleanupReceipt) throw new Error("cleanup receipt fixture unavailable");
      v.cleanupReceipt.captureRef = "koc_foreignxx";
    },
    (v: KubernetesCaptureReadback) => {
      if (!v.cleanupReceipt) throw new Error("cleanup receipt fixture unavailable");
      v.cleanupReceipt.repairedCount = 101;
    },
    (v: KubernetesCaptureReadback) => {
      if (!v.cleanupReceipt) throw new Error("cleanup receipt fixture unavailable");
      v.cleanupReceipt.version = 3;
    },
  ]) {
    const copy = structuredClone(original);
    mutate(copy);
    expect(() =>
      parseKubernetesCaptureReadback(
        copy,
        context,
        original.view.summary.captureRef,
      ),
    ).toThrow(KubernetesCaptureProtocolError);
  }
});
