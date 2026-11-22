import { describe, it, expect } from "vitest";
import {
  parseKubernetesFileInput,
  parseKubernetesFileInputList,
  parseKubernetesFileCleanupReadback,
  parseKubernetesFileCleanupList,
  parseKubernetesFileSourceReadback,
} from "./kubernetes-file-input";
import { fileInputFixture } from "./kubernetes-file-input.test-support";
describe("Kubernetes FILE immutable metadata boundary", () => {
  it("retains historical READY without claiming remote success", () => {
    const value = fileInputFixture();
    expect(
      parseKubernetesFileInput(value, value.invocationRef, value.sourceRef),
    ).toBe(value);
    expect(
      parseKubernetesFileInputList(
        { projection: "SOURCE", items: [value] },
        value.invocationRef,
      ),
    ).toEqual([value]);
  });
  it("rejects missing, unknown, foreign, oversized and contradictory metadata", () => {
    const value = fileInputFixture();
    for (const invalid of [
      { ...value, sourceRef: "kfs_foreign00" },
      { ...value, invocationRef: "inv_foreign00" },
      { ...value, payload: "private" },
      { ...value, normalizedSHA256: "" },
      { ...value, artifactSizeBytes: 16777217 },
      { ...value, stagedBytes: 2049 },
      { ...value, acknowledgedBytes: 1025 },
      { ...value, eofAcknowledged: true },
      { ...value, state: "SUCCEEDED" },
      { ...value, reason: "raw-provider-error" },
      { ...value, version: Number.MAX_SAFE_INTEGER + 1 },
      { ...value, transferFormat: ["FILE"] },
      { ...value, hardDeadline: value.createdAt },
      { ...value, hardDeadline: "2026-11-17" },
      { ...value, normalizationRevision: 2 },
      Object.fromEntries(
        Object.entries(value).filter(([key]) => key !== "eofAcknowledged"),
      ),
    ])
      expect(() =>
        parseKubernetesFileInput(invalid, value.invocationRef, value.sourceRef),
      ).toThrow();
    for (const invalid of [
      { items: [value, value] },
      { items: Array(257).fill(value) },
      { items: [], next: "cursor" },
    ])
      expect(() =>
        parseKubernetesFileInputList(invalid, value.invocationRef),
      ).toThrow();
  });
});

describe("FILE cleanup-only projection", () => {
  const minimal = () => ({
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
  });
  it("has no representable source provenance and requires explicit projection", () => {
    const view = minimal();
    expect(
      parseKubernetesFileCleanupList(
        { projection: "CLEANUP_ONLY", items: [view] },
        view.invocationRef,
      ),
    ).toEqual([view]);
    for (const invalid of [
      { ...view, artifactRef: "art_hidden" },
      { ...view, manifest: [] },
      { ...view, invocationRef: "inv_foreign00" },
      { ...view, cleanup: { ...view.cleanup, outstandingCount: 2 } },
      { ...view, cleanup: { ...view.cleanup, outstandingBytes: 16777217 } },
      { ...view, cleanup: { ...view.cleanup, exhaustedCount: 0 } },
    ])
      expect(() =>
        parseKubernetesFileCleanupList(
          { projection: "CLEANUP_ONLY", items: [invalid] },
          view.invocationRef,
        ),
      ).toThrow();
    expect(() =>
      parseKubernetesFileInputList(
        { projection: "CLEANUP_ONLY", items: [view] },
        view.invocationRef,
      ),
    ).toThrow();
    expect(() =>
      parseKubernetesFileCleanupList(
        { projection: "SOURCE", items: [fileInputFixture()] },
        view.invocationRef,
      ),
    ).toThrow();
    expect(() =>
      parseKubernetesFileSourceReadback(
        { projection: "SOURCE", view: fileInputFixture(), cleanupView: view },
        view.invocationRef,
        view.sourceRef,
      ),
    ).toThrow();
  });
  it("binds original repair receipt to current cleanup counters without claiming deletion", () => {
    const view = minimal();
    view.cleanup.version = 4;
    view.cleanup.repairGeneration = 1;
    const receipt = {
      sourceRef: view.sourceRef,
      version: 4,
      repairGeneration: 1,
      repairedCount: 1,
      remainingExhaustedCount: 0,
      acceptedAt: "2026-11-17T00:00:00Z",
    };
    const result = {
      projection: "CLEANUP_ONLY",
      cleanupView: view,
      cleanupReceipt: receipt,
    };
    expect(
      parseKubernetesFileCleanupReadback(
        result,
        view.invocationRef,
        view.sourceRef,
        true,
      ),
    ).toBe(result);
    for (const altered of [
      { ...receipt, sourceRef: "kfs_foreign00" },
      { ...receipt, version: 5 },
      { ...receipt, repairedCount: 2 },
      { ...receipt, repairGeneration: 2 },
      { ...receipt, acceptedAt: "2099-01-01T00:00:00Z" },
      { ...receipt, artifactRef: "art_hidden" },
    ])
      expect(() =>
        parseKubernetesFileCleanupReadback(
          { ...result, cleanupReceipt: altered },
          view.invocationRef,
          view.sourceRef,
          true,
        ),
      ).toThrow();
    expect(() =>
      parseKubernetesFileCleanupReadback(
        { projection: "CLEANUP_ONLY", cleanupView: view },
        view.invocationRef,
        view.sourceRef,
        true,
      ),
    ).toThrow();
  });
});
