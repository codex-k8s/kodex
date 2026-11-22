import { describe, expect, it } from "vitest";
import type { KubernetesArtifactCaptureOrigin } from "./generated/openapi/types.gen";
import { assertArtifactCaptureOrigin } from "./kubernetes-capture-origin";

import { captureOriginFixture } from "./kubernetes-capture-origin.fixture";

describe("сохранённая квитанция Kubernetes", () => {
  it("сохраняет partial/remote-zero и точный bigint ordinal без округления", () => {
    const value = captureOriginFixture();
    value.acknowledgedPrefix = "9007199254740993";
    value.outputWatermark = "9007199254740994";
    expect(assertArtifactCaptureOrigin(value)).toBe(value);
    expect(value.acknowledgedPrefix).toBe("9007199254740993");
    expect(assertArtifactCaptureOrigin(undefined)).toBeUndefined();
  });
  it("отклоняет null, массив и отсутствующий resource до чтения полей", () => {
    for (const value of [
      null,
      [],
      { ...captureOriginFixture(), resource: null },
    ]) {
      expect(() =>
        assertArtifactCaptureOrigin(
          value as unknown as KubernetesArtifactCaptureOrigin,
        ),
      ).toThrow();
    }
  });
  it("не повышает partial либо неподтверждённый join до полного результата", () => {
    for (const change of [
      (value: KubernetesArtifactCaptureOrigin) => {
        value.kind = "FULL";
      },
      (value: KubernetesArtifactCaptureOrigin) => {
        value.nativeTerminalKnown = false;
      },
      (value: KubernetesArtifactCaptureOrigin) => {
        value.remoteOutcome = "UNKNOWN";
      },
      (value: KubernetesArtifactCaptureOrigin) => {
        value.acknowledgedPrefix = "9223372036854775808";
      },
      (value: KubernetesArtifactCaptureOrigin) => {
        value.sourceDigest = "sha256:forged";
      },
    ]) {
      const value = captureOriginFixture();
      change(value);
      expect(() => assertArtifactCaptureOrigin(value)).toThrow();
    }
  });
});
