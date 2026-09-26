import { describe, expect, it } from "vitest";

import { isAssistantRoleImageBuildDebugRequest } from "@/features/assistant/events";

const valid = {
  kind: "ROLE_IMAGE_BUILD_DEBUG",
  recipeRef: "imgrec_example01",
  buildRef: "imgbld_example01",
  attempt: 3,
  stage: "DEAD_LETTER",
  safeErrorCode: "INSTALLATION_FAILED",
  diagnosticCode: "INSTALL_COMMAND_REJECTED",
  diagnosticSummary: "Installation step failed",
};

describe("запрос диагностики сборки в помощнике", () => {
  it("принимает точную безопасную terminal-попытку", () => {
    expect(isAssistantRoleImageBuildDebugRequest(valid)).toBe(true);
  });

  it.each([
    { recipeRef: "../wrong" },
    { buildRef: "../wrong" },
    { attempt: 0 },
    { attempt: 1.5 },
    { stage: "COMPLETED" },
    { safeErrorCode: "unsafe\ncode" },
    { safeErrorCode: 123 },
    { diagnosticCode: 123 },
    { diagnosticSummary: "x".repeat(257) },
  ])("отклоняет повреждённые поля %j", (change) => {
    expect(isAssistantRoleImageBuildDebugRequest({ ...valid, ...change })).toBe(
      false,
    );
  });
});
