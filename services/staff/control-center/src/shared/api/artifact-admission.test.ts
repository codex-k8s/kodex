import { describe, expect, it } from "vitest";
import { artifactContentAdmitted } from "./artifact-admission";

describe("допуск обычных файлов", () => {
  it("распознаёт явное отсутствие AV, сохраняя закрытый отказ остальных состояний", () => {
    expect(artifactContentAdmitted("CLEAN")).toBe(true);
    expect(artifactContentAdmitted("NOT_SCANNED")).toBe(true);
    for (const state of [
      "",
      "PENDING",
      "SCANNING",
      "QUARANTINED",
      "FAILED",
      "clean",
      "UNKNOWN",
    ])
      expect(artifactContentAdmitted(state)).toBe(false);
  });
});
